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

// A client may send a string, one message, or a snapshot of prior messages.
func extractConversationRequest(request map[string]any) model.ConversationExtraction {
	if nested, ok := request["response"].(map[string]any); ok {
		request = nested
	}
	ex := model.ConversationExtraction{}
	for _, key := range []string{"conversation_id", "conversation", "session_id"} {
		value := request[key]
		if m, ok := value.(map[string]any); ok {
			value = m["id"]
		}
		if id, ok := value.(string); ok && strings.TrimSpace(id) != "" {
			ex.ClientKey = model.ConversationHash(key + "|" + id)
			break
		}
	}
	ex.PrevResponseId, _ = request["previous_response_id"].(string)
	input := request["input"]
	if input == nil {
		input = request["messages"]
	}
	var items []any
	switch v := input.(type) {
	case string:
		items = []any{map[string]any{"role": "user", "content": v}}
	case []any:
		items = v
	}
	ex.ItemCount = len(items)
	lastQuestion := -1
	lastActivity := -1
	for i, raw := range items {
		item, _ := raw.(map[string]any)
		role, _ := item["role"].(string)
		if role != "user" && role != "assistant" {
			if role == "tool" || (role == "" && item["type"] != nil) {
				lastActivity = i
			}
			continue
		}
		if role == "assistant" {
			lastActivity = i
		}
		add := func(text string, part int) {
			segments := []model.ConversationUserItem{{Role: "assistant", Kind: "text", Text: text, Hash: model.ConversationHash(text)}}
			if role == "user" {
				segments = model.ConversationInputParts(text)
			}
			for _, v := range segments {
				if strings.TrimSpace(v.Text) == "" {
					continue
				}
				v.Item = i
				v.Part = part
				ex.UserItems = append(ex.UserItems, v)
				if v.IsQuestion() {
					lastQuestion = i
				}
			}
		}
		switch content := item["content"].(type) {
		case string:
			add(content, 0)
		case []any:
			for j, raw := range content {
				switch part := raw.(type) {
				case string:
					add(part, j)
				case map[string]any:
					t, _ := part["type"].(string)
					if t == "text" || t == "input_text" || t == "output_text" || t == "" {
						if s, ok := part["text"].(string); ok {
							add(s, j)
						}
					}
				}
			}
		}
	}
	for i := range ex.UserItems {
		p := &ex.UserItems[i]
		p.Origin = "request"
		if p.Role == "assistant" || p.Item < lastQuestion || p.Item < lastActivity {
			p.Origin = "snapshot"
			ex.Snapshot = true
		}
	}
	if lastActivity >= 0 {
		ex.Snapshot = true
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
	visible    visibleConversationOutput
	line       []byte
	raw        bytes.Buffer
	sawEvent   bool
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
	r.observeEvent(event, nil)
	a.visible.observe(event)
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
		} // The legacy event budget must not stop visible-text capture.
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
			r.observeEvent(full, nil)
			a.visible.observe(full)
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
	return visibleText(a.visible.parts(r.entry.Outcome, r.entry.Truncated)), final
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
	legacyRows := model.DB.Migrator().HasTable("conversation_messages")
	legacyTools := model.DB.Migrator().HasColumn("conversation_logs", "tool_calls")
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
				model.DB.Model(&model.ConversationLog{}).Where("request_id = ?", row.RequestId).Update("replay_state", "error")
				continue
			}
			ex, toolCalls := recordedConversation(entry, content)
			if legacyTools {
				var prior int
				if err := model.DB.Table("conversation_logs").Select("COALESCE(tool_calls,0)").Where("request_id = ?", row.RequestId).Scan(&prior).Error; err != nil {
					common.SysError("conversation legacy tool count read failed")
					return
				}
				toolCalls = max(toolCalls, prior)
			}
			if legacyRows && ex.Placeholder {
				var old []model.ConversationMessage
				if err := model.DB.Table("conversation_messages").Select("id,role,kind,content,content_hash,truncated").Where("request_id = ?", row.RequestId).Order("id").Find(&old).Error; err != nil {
					common.SysError("conversation legacy evidence read failed")
					return
				}
				for i, raw := range old {
					if raw.Role == "user" {
						for _, p := range model.ConversationInputParts(raw.Content) {
							p.Origin = "legacy"
							p.Item = i
							p.Truncated = raw.Truncated
							if p.Kind == "text" {
								p.Kind = "unknown"
							}
							ex.UserItems = append(ex.UserItems, p)
						}
					} else if raw.Role == "assistant" && content.Response == "" && len(ex.Outputs) == 0 {
						ex.Outputs = append(ex.Outputs, model.ConversationUserItem{Role: "assistant", Origin: "legacy", Kind: "unknown", Text: raw.Content, Truncated: raw.Truncated})
					}
				}
			}
			if err := model.StoreSessionConversation(entry, &ex, "", toolCalls, conversationTextMax(), nil, model.AuditLog{}, true); err != nil {
				common.SysError("conversation session backfill store failed; request_id=" + row.RequestId)
				model.DB.Model(&model.ConversationLog{}).Where("request_id = ?", row.RequestId).Update("replay_state", "error")
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
