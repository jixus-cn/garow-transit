package service

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const conversationContextKey = "garow_conversation_capture"

type conversationWrite struct {
	entry         model.ConversationLog
	content       model.ConversationContent
	audit         model.AuditLog
	extraction    *model.ConversationExtraction
	assistantText string
	toolCalls     int
	legacy        bool
	backfill      bool
	textMax       int
}

var conversationQueue = make(chan conversationWrite, conversationQueueSize())
var conversationSlots = make(chan struct{}, conversationInflight())
var conversationStart sync.Once
var conversationDropped atomic.Uint64

func ConversationLoggingEnabled() bool { return os.Getenv("CONVERSATION_LOG_ENABLED") == "true" }

// StartConversationRecorder owns bounded asynchronous persistence and expiry.
func StartConversationRecorder() {
	conversationStart.Do(func() {
		go func() {
			if err := model.BackfillConversationLinks(); err != nil {
				common.SysError("conversation link backfill failed")
			}
			if ConversationSessionMode() {
				go BackfillConversationSessions()
			}
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for {
				select {
				case item := <-conversationQueue:
					var user model.User
					if model.DB.Select("username", "role").Where("id = ?", item.entry.UserId).First(&user).Error == nil {
						item.audit.Username = user.Username
						item.audit.ActorRole = user.Role
					}
					if item.audit.Username == "" {
						item.audit.Username = "user#" + common.Interface2String(item.entry.UserId)
					}
					var err error
					if item.extraction != nil {
						var legacy *model.ConversationContent
						if item.legacy {
							legacy = &item.content
						}
						err = model.StoreSessionConversation(&item.entry, item.extraction, item.assistantText, item.toolCalls, item.textMax, legacy, item.audit, item.backfill)
					} else {
						err = model.StoreConversation(&item.entry, item.content, item.audit)
					}
					if err != nil {
						conversationDropped.Add(1)
						common.SysError("conversation persistence failed; request_id=" + item.entry.RequestId)
					}
				case <-ticker.C:
					if err := model.CleanupConversations(model.ConversationCutoff()); err != nil {
						common.SysError("conversation retention cleanup failed")
					}
					if count := conversationDropped.Swap(0); count > 0 {
						common.SysError("conversation records dropped: " + common.Interface2String(count))
					}
				}
			}
		}()
	})
}

// ConversationCapture never owns a Gin context after the request ends.
type ConversationCapture struct {
	mu         sync.Mutex
	entry      model.ConversationLog
	content    model.ConversationContent
	audit      model.AuditLog
	response   bytes.Buffer
	limit      int
	finished   bool
	session    bool
	legacy     bool
	textMax    int
	extraction model.ConversationExtraction
	agg        conversationAggregator
}

func BeginConversation(c *gin.Context, protocol string) *ConversationCapture {
	if !ConversationLoggingEnabled() || c.GetInt("id") <= 0 {
		return nil
	}
	select {
	case conversationSlots <- struct{}{}:
	default:
		conversationDropped.Add(1)
		return nil
	}
	limit := min(max(common.GetEnvOrDefault("CONVERSATION_LOG_MAX_BYTES", 2<<20), 64<<10), 8<<20)
	r := &ConversationCapture{limit: limit}
	r.session = ConversationSessionMode()
	r.legacy = r.session && conversationLegacyPayload()
	r.textMax = conversationTextMax()
	if r.session {
		r.agg.cap = conversationSafetyBytes()
		r.agg.legacy = r.legacy
	}
	r.entry = model.ConversationLog{RequestId: c.GetString(common.RequestIdKey), UserId: c.GetInt("id"), CreatedAt: time.Now().Unix(), TokenId: c.GetInt(string(constant.ContextKeyTokenId)), TokenName: c.GetString("token_name"), Protocol: protocol}
	r.entry.Kind = "chat"
	if protocol == "websocket" || strings.HasSuffix(strings.TrimSuffix(c.Request.URL.Path, "/"), "/responses") {
		r.entry.Kind = "responses"
	}
	if r.entry.RequestId == "" {
		r.entry.RequestId = common.NewRequestId()
	}
	r.audit = model.AuditLog{RequestId: r.entry.RequestId, EventId: r.entry.RequestId, UserId: r.entry.UserId, Username: c.GetString("username"), CreatedAt: r.entry.CreatedAt, Category: model.AuditCategoryConversation, Action: "conversation.record", Ip: c.ClientIP(), Method: c.Request.Method, Route: c.Request.URL.Path, AuthMethod: "api_key"}
	storage, err := common.GetBodyStorage(c)
	if err == nil {
		readMax := limit
		if r.session {
			readMax = conversationParseMax()
		}
		var body []byte
		body, err = io.ReadAll(io.LimitReader(storage, int64(readMax)+1))
		_, seekErr := storage.Seek(0, io.SeekStart)
		c.Request.Body = io.NopCloser(storage)
		if seekErr != nil {
			err = seekErr
		}
		if r.session {
			r.extraction.Placeholder = true
			if len(body) > readMax {
				r.entry.Truncated = true
				if r.legacy {
					r.content.Request = `{"capture":"request exceeds recording limit"}`
				}
			} else if err == nil {
				var request map[string]any
				if common.Unmarshal(body, &request) == nil {
					r.entry.Model, _ = request["model"].(string)
					r.extraction = extractConversationRequest(request)
					if r.legacy {
						if len(body) > limit {
							r.content.Request = `{"capture":"request exceeds recording limit"}`
						} else {
							filtered := map[string]any{}
							for _, key := range []string{"model", "messages", "instructions", "input", "tools", "tool_choice", "reasoning", "previous_response_id", "stream", "type", "response"} {
								if value, ok := request[key]; ok {
									filtered[key] = value
								}
							}
							r.content.LatestInput = conversationLatestInput(sanitizeConversation(filtered, 0).(map[string]any))
							encoded, _ := common.Marshal(sanitizeConversation(filtered, 0))
							r.content.Request = string(encoded)
						}
					}
				}
			}
		} else if len(body) > limit {
			r.entry.Truncated = true
			r.content.Request = `{"capture":"request exceeds recording limit"}`
		} else if err == nil {
			var request map[string]any
			if common.Unmarshal(body, &request) == nil {
				r.entry.Model, _ = request["model"].(string)
				filtered := map[string]any{}
				for _, key := range []string{"model", "messages", "instructions", "input", "tools", "tool_choice", "reasoning", "previous_response_id", "stream", "type", "response"} {
					if value, ok := request[key]; ok {
						filtered[key] = value
					}
				}
				r.content.LatestInput = conversationLatestInput(sanitizeConversation(filtered, 0).(map[string]any))
				encoded, _ := common.Marshal(sanitizeConversation(filtered, 0))
				r.content.Request = string(encoded)
			}
		}
	}
	if err != nil || r.content.Request == "" {
		if !r.session || r.legacy {
			r.content.Request = `{"capture":"request unavailable"}`
		}
		if r.session && err != nil {
			r.extraction.Placeholder = true
		}
	}
	c.Set(conversationContextKey, r)
	return r
}

func ConversationFromContext(c *gin.Context) *ConversationCapture {
	value, _ := c.Get(conversationContextKey)
	r, _ := value.(*ConversationCapture)
	return r
}

// Observe only bytes successfully written to the client. WebSocket callers pass
// one JSON message at a time with a trailing newline.
func (r *ConversationCapture) Observe(data []byte) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	if r.session {
		r.observeSessionData(data)
		return
	}
	remaining := r.limit - r.response.Len()
	if len(data) > remaining {
		r.entry.Truncated = true
		data = data[:remaining]
	}
	r.response.Write(data)
}

func (r *ConversationCapture) Finish(status int, cancelled bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.finished = true
	defer func() { <-conversationSlots }()
	if r.session {
		output, final := r.finishSessionAggregation()
		if r.entry.Outcome == "" {
			r.entry.Outcome = "incomplete"
		}
		if cancelled {
			r.entry.Outcome = "interrupted"
		}
		if status >= 400 {
			r.entry.Outcome = "failed"
		}
		r.entry.ResponseId = r.agg.responseId
		r.entry.PreviousResponseId = r.extraction.PrevResponseId
		if r.legacy {
			encoded, _ := common.Marshal(sanitizeConversation(final, 0))
			r.content.Response = string(encoded)
			r.content.Output = output
		}
		r.audit.Status = status
		r.audit.Success = r.entry.Outcome == "completed" && !r.entry.Truncated
		r.audit.Content = "Conversation · " + r.entry.Model
		r.audit.ActorRole = common.RoleCommonUser
		extraction := r.extraction
		select {
		case conversationQueue <- conversationWrite{entry: r.entry, content: r.content, audit: r.audit, extraction: &extraction, assistantText: output, toolCalls: r.agg.toolCalls, legacy: r.legacy, textMax: r.textMax}:
		default:
			conversationDropped.Add(1)
		}
		return
	}
	r.entry.Outcome = "incomplete"
	if status >= 400 {
		r.entry.Outcome = "failed"
	}
	if cancelled {
		r.entry.Outcome = "interrupted"
	}
	var events []any
	var output strings.Builder
	var final any
	var full map[string]any
	if common.Unmarshal(r.response.Bytes(), &full) == nil {
		final = full
		r.observeEvent(full, &output)
	} else {
		// SSE frames may span many writes; split only after the bounded capture
		// is complete. Blank-line event boundaries are preserved in stored data.
		for line := range strings.SplitSeq(r.response.String(), "\n") {
			line = strings.TrimSpace(line)
			if line == "data: [DONE]" {
				if r.entry.Outcome != "failed" {
					r.entry.Outcome = "completed"
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
			var event map[string]any
			if common.Unmarshal([]byte(line), &event) != nil {
				continue
			}
			r.observeEvent(event, &output)
			if len(events) < 4096 {
				events = append(events, sanitizeConversation(event, 0))
			} else {
				r.entry.Truncated = true
			}
			if kind, _ := event["type"].(string); kind == "response.completed" || kind == "response.done" {
				if response, ok := event["response"].(map[string]any); ok {
					final = response
				}
			}
		}
	}
	if final == nil {
		final = events
	}
	if cancelled {
		r.entry.Outcome = "interrupted"
	}
	if status >= 400 {
		r.entry.Outcome = "failed"
	}
	encoded, _ := common.Marshal(sanitizeConversation(final, 0))
	r.content.Response = string(encoded)
	r.content.Output = output.String()
	// A terminal Responses object contains readable text even when the server
	// sends no delta events. Do not duplicate text already received as deltas.
	if r.content.Output == "" {
		r.content.Output = conversationText(final)
	}
	r.audit.Status = status
	r.audit.Success = r.entry.Outcome == "completed" && !r.entry.Truncated
	r.audit.Content = "Conversation · " + r.entry.Model
	r.audit.ActorRole = common.RoleCommonUser
	select {
	case conversationQueue <- conversationWrite{entry: r.entry, content: r.content, audit: r.audit}:
	default:
		conversationDropped.Add(1)
	}
}

func (r *ConversationCapture) observeEvent(event map[string]any, output *strings.Builder) {
	if m, ok := event["model"].(string); ok {
		r.entry.ResponseModel = m
	}
	if response, ok := event["response"].(map[string]any); ok {
		if m, ok := response["model"].(string); ok {
			r.entry.ResponseModel = m
		}
	}
	kind, _ := event["type"].(string)
	switch kind {
	case "response.output_text.delta", "response.refusal.delta":
		if delta, ok := event["delta"].(string); ok {
			output.WriteString(delta)
		}
	case "response.completed", "response.done":
		r.entry.Outcome = "completed"
	case "error", "response.failed":
		r.entry.Outcome = "failed"
	case "response.incomplete", "response.cancelled", "response.canceled":
		r.entry.Outcome = "incomplete"
	}
	if _, ok := event["error"]; ok {
		r.entry.Outcome = "failed"
	}
	if choices, ok := event["choices"].([]any); ok {
		for _, choice := range choices {
			item, _ := choice.(map[string]any)
			if delta, ok := item["delta"].(map[string]any); ok {
				output.WriteString(conversationText(delta["content"]))
			}
			if message, ok := item["message"].(map[string]any); ok {
				output.WriteString(conversationText(message["content"]))
			}
			if reason, ok := item["finish_reason"].(string); ok && reason != "" {
				r.entry.Outcome = "completed"
			}
		}
	}
	switch status, _ := event["status"].(string); status {
	case "completed":
		if r.entry.Outcome != "failed" {
			r.entry.Outcome = "completed"
		}
	case "failed":
		r.entry.Outcome = "failed"
	case "incomplete", "cancelled", "canceled":
		r.entry.Outcome = "incomplete"
	}
}

func conversationLatestInput(request map[string]any) string {
	if nested, ok := request["response"].(map[string]any); ok {
		return conversationLatestInput(nested)
	}
	for _, key := range []string{"messages", "input"} {
		if value, ok := request[key].(string); ok {
			return value
		}
		if items, ok := request[key].([]any); ok {
			for i := len(items) - 1; i >= 0; i-- {
				if item, ok := items[i].(map[string]any); ok && item["role"] == "user" {
					return conversationText(item["content"])
				}
			}
		}
	}
	return ""
}

func conversationText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var text strings.Builder
		for _, item := range v {
			text.WriteString(conversationText(item))
		}
		return text.String()
	case map[string]any:
		for _, key := range []string{"text", "content", "output", "refusal"} {
			if item, ok := v[key]; ok {
				return conversationText(item)
			}
		}
	}
	return ""
}

func sanitizeConversation(value any, depth int) any {
	if depth > 64 {
		return "[nested content omitted]"
	}
	switch v := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(v))
		for key, item := range v {
			switch strings.ToLower(key) {
			case "authorization", "cookie", "set-cookie", "api_key", "api-key", "x-api-key", "access_token", "refresh_token", "password", "encrypted_content", "image_url", "audio", "data", "file_data", "file_url", "headers":
				clean[key] = "[omitted]"
			default:
				clean[key] = sanitizeConversation(item, depth+1)
			}
		}
		return clean
	case []any:
		clean := make([]any, len(v))
		for i, item := range v {
			clean[i] = sanitizeConversation(item, depth+1)
		}
		return clean
	case string:
		if strings.HasPrefix(v, "data:") {
			return "[binary content omitted]"
		}
	}
	return value
}
