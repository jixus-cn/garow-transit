package service

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Session mode is the default; CONVERSATION_LOG_MODE=full restores the legacy
// per-request capture path byte-for-byte for rollback.
func ConversationSessionMode() bool { return os.Getenv("CONVERSATION_LOG_MODE") != "full" }

// Legacy payload dual-write stays on until an operator verifies the session
// model and sets CONVERSATION_LOG_PAYLOAD=off.
func conversationLegacyPayload() bool { return os.Getenv("CONVERSATION_LOG_PAYLOAD") != "off" }

func conversationParseMax() int {
	return min(max(common.GetEnvOrDefault("CONVERSATION_LOG_PARSE_MAX_BYTES", 32<<20), 64<<10), 128<<20)
}

func conversationTextMax() int {
	return min(max(common.GetEnvOrDefault("CONVERSATION_TEXT_MAX_BYTES", 64<<10), 1<<10), 1<<20)
}

func conversationSafetyBytes() int {
	return min(max(common.GetEnvOrDefault("CONVERSATION_RESPONSE_SAFETY_BYTES", 16<<20), 1<<20), 64<<20)
}

func conversationQueueSize() int {
	return min(max(common.GetEnvOrDefault("CONVERSATION_LOG_QUEUE_SIZE", 256), 16), 4096)
}

func conversationInflight() int {
	return min(max(common.GetEnvOrDefault("CONVERSATION_LOG_MAX_INFLIGHT", 128), 8), 1024)
}

var conversationContextPrefixes = []string{
	"<environment_context>", "<user_instructions>", "<system-reminder>",
	"<permissions instructions>", "<turn_aborted>", "<context>",
}

func isConversationContext(text string) bool {
	trimmed := strings.TrimSpace(text)
	for _, prefix := range conversationContextPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// extractConversationRequest pulls session identity and user-role items from a
// parsed request. Hashes cover full text; storage truncation happens later.
func extractConversationRequest(request map[string]any) model.ConversationExtraction {
	if nested, ok := request["response"].(map[string]any); ok {
		request = nested
	}
	ex := model.ConversationExtraction{}
	for _, key := range []string{"prompt_cache_key", "session_id", "conversation_id"} {
		if value, ok := request[key].(string); ok && value != "" {
			ex.ClientKey = value
			break
		}
	}
	if ex.ClientKey == "" {
		if meta, ok := request["metadata"].(map[string]any); ok {
			ex.ClientKey, _ = meta["user_id"].(string)
		}
	}
	ex.PrevResponseId, _ = request["previous_response_id"].(string)
	var items []any
	switch value := request["input"].(type) {
	case []any:
		items = value
	case string:
		if value != "" {
			ex.UserItems = append(ex.UserItems, model.ConversationUserItem{Hash: model.ConversationHash(value), Text: value})
		}
		ex.ItemCount = 1
		return ex
	}
	if items == nil {
		switch value := request["messages"].(type) {
		case []any:
			items = value
		case string:
			if value != "" {
				ex.UserItems = append(ex.UserItems, model.ConversationUserItem{Hash: model.ConversationHash(value), Text: value})
			}
			ex.ItemCount = 1
			return ex
		}
	}
	ex.ItemCount = len(items)
	lastUser := -1
	for i, item := range items {
		if m, ok := item.(map[string]any); ok && m["role"] == "user" {
			lastUser = i
		}
	}
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok || m["role"] != "user" {
			continue
		}
		text := conversationText(m["content"])
		ex.UserItems = append(ex.UserItems, model.ConversationUserItem{
			Hash:    model.ConversationHash(text),
			Text:    text,
			Context: isConversationContext(text) || (i != lastUser && len(text) > 4000),
		})
	}
	return ex
}

var conversationToolItemTypes = map[string]bool{
	"function_call": true, "custom_tool_call": true, "local_shell_call": true,
	"computer_call": true, "code_interpreter_call": true, "mcp_call": true,
	"file_search_call": true, "web_search_call": true, "image_generation_call": true,
}

// conversationAggregator parses response events incrementally so memory stays
// O(visible text) instead of O(all SSE bytes). Reasoning deltas never enter the
// text budget. Legacy dual-write additionally keeps the sanitized event list.
type conversationAggregator struct {
	line       []byte
	raw        bytes.Buffer
	sawEvent   bool
	output     strings.Builder
	events     []any
	eventBytes int
	final      any
	responseId string
	toolCalls  int
	chatTools  map[int]bool
	seen       int
	cap        int
	truncated  bool
	legacy     bool
}

func (r *ConversationCapture) observeSessionData(data []byte) {
	a := &r.agg
	if a.truncated {
		return
	}
	a.seen += len(data)
	if a.seen > a.cap {
		a.truncated = true
		r.entry.Truncated = true
		return
	}
	if !a.sawEvent {
		a.raw.Write(data)
	}
	a.line = append(a.line, data...)
	for {
		idx := bytes.IndexByte(a.line, '\n')
		if idx < 0 {
			break
		}
		line := a.line[:idx]
		a.line = a.line[idx+1:]
		r.observeSessionLine(line)
	}
}

func (r *ConversationCapture) observeSessionLine(line []byte) {
	a := &r.agg
	text := strings.TrimSpace(string(line))
	if text == "" {
		return
	}
	if text == "data: [DONE]" {
		if r.entry.Outcome != "failed" {
			r.entry.Outcome = "completed"
		}
		return
	}
	if strings.HasPrefix(text, "data:") {
		text = strings.TrimSpace(strings.TrimPrefix(text, "data:"))
	}
	var event map[string]any
	if common.Unmarshal([]byte(text), &event) != nil {
		return
	}
	a.sawEvent = true
	a.raw.Reset()
	r.observeEvent(event, &a.output)
	kind, _ := event["type"].(string)
	if kind == "" {
		a.final = event
		if id, ok := event["id"].(string); ok && id != "" {
			a.responseId = id
		}
	}
	if kind == "response.output_item.added" {
		if item, ok := event["item"].(map[string]any); ok {
			if itemType, _ := item["type"].(string); conversationToolItemTypes[itemType] {
				a.toolCalls++
			}
		}
	}
	if response, ok := event["response"].(map[string]any); ok {
		if id, ok := response["id"].(string); ok && id != "" {
			a.responseId = id
		}
	}
	if kind == "response.completed" || kind == "response.done" {
		if response, ok := event["response"].(map[string]any); ok {
			a.final = response
		}
	}
	if choices, ok := event["choices"].([]any); ok {
		for _, choice := range choices {
			item, _ := choice.(map[string]any)
			delta, _ := item["delta"].(map[string]any)
			if calls, ok := delta["tool_calls"].([]any); ok {
				if a.chatTools == nil {
					a.chatTools = map[int]bool{}
				}
				for _, call := range calls {
					if cm, ok := call.(map[string]any); ok {
						if idx, ok := cm["index"].(float64); ok && !a.chatTools[int(idx)] {
							a.chatTools[int(idx)] = true
							a.toolCalls++
						}
					}
				}
			}
		}
	}
	if a.legacy {
		if len(a.events) < 4096 && a.eventBytes < 2<<20 {
			sanitized := sanitizeConversation(event, 0)
			encoded, _ := common.Marshal(sanitized)
			a.events = append(a.events, sanitized)
			a.eventBytes += len(encoded)
		} else {
			a.truncated = true
			r.entry.Truncated = true
		}
	}
}

// finishSessionAggregation resolves the terminal object and visible text. A
// plain JSON body (no line events) is parsed whole, matching legacy semantics.
func (r *ConversationCapture) finishSessionAggregation() (string, any) {
	a := &r.agg
	if len(a.line) > 0 {
		r.observeSessionLine(a.line)
		a.line = nil
	}
	if !a.sawEvent {
		var full map[string]any
		if common.Unmarshal(a.raw.Bytes(), &full) == nil {
			r.observeEvent(full, &a.output)
			a.final = full
			if id, ok := full["id"].(string); ok {
				a.responseId = id
			}
		}
	}
	final := a.final
	if final == nil && a.legacy {
		final = a.events
	}
	output := a.output.String()
	if output == "" {
		output = conversationText(final)
	}
	return output, final
}

var conversationBackfillMu sync.Mutex

// BackfillConversationSessions maps legacy payload rows into the session model.
// The pass is synchronous and mutex-serialized so the startup goroutine and any
// manual call never double-process a row; state markers make it idempotent and
// crash-resumable. Positional prefix matching assigns ancestor rows to sessions
// already created by newer live requests.
func BackfillConversationSessions() {
	conversationBackfillMu.Lock()
	defer conversationBackfillMu.Unlock()
	processed := 0
	for {
		rows, err := model.PendingConversationBackfill(100)
		if err != nil {
			common.SysError("conversation session backfill query failed")
			return
		}
		if len(rows) == 0 {
			if processed > 0 {
				common.SysLog("conversation session backfill completed: " + common.Interface2String(processed))
			}
			return
		}
		for _, row := range rows {
			entry, content, err := model.GetConversation(row.RequestId, 0)
			if err != nil {
				common.SysError("conversation session backfill read failed; request_id=" + row.RequestId)
				model.DB.Model(&model.ConversationLog{}).Where("request_id = ?", row.RequestId).Update("session_state", "error")
				continue
			}
			ex := model.ConversationExtraction{Placeholder: true, PrevResponseId: entry.PreviousResponseId}
			var request map[string]any
			if common.UnmarshalJsonStr(content.Request, &request) == nil {
				if _, marker := request["capture"]; !marker {
					ex = extractConversationRequest(request)
					if ex.PrevResponseId == "" {
						ex.PrevResponseId = entry.PreviousResponseId
					}
				}
			}
			toolCalls := 0
			var events []any
			if common.UnmarshalJsonStr(content.Response, &events) == nil {
				for _, value := range events {
					if event, ok := value.(map[string]any); ok {
						if kind, _ := event["type"].(string); kind == "response.output_item.added" {
							if item, ok := event["item"].(map[string]any); ok {
								if itemType, _ := item["type"].(string); conversationToolItemTypes[itemType] {
									toolCalls++
								}
							}
						}
					}
				}
			}
			if err := model.StoreSessionConversation(entry, &ex, content.Output, toolCalls, conversationTextMax(), nil, model.AuditLog{}, true); err != nil {
				common.SysError("conversation session backfill store failed; request_id=" + row.RequestId)
				model.DB.Model(&model.ConversationLog{}).Where("request_id = ?", row.RequestId).Update("session_state", "error")
				continue
			}
			processed++
		}
	}
}

// FlushConversations drains the write queue during graceful shutdown so planned
// restarts do not lose captured turns. Crashes still lose queued items.
func FlushConversations(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for len(conversationQueue) > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
}
