package service

import (
	"context"
	"fmt"
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
	entry   model.QARequestMeta
	facts   model.QAFacts
	drained chan struct{}
	weight  int64
}

var conversationQueue = make(chan conversationWrite, min(max(common.GetEnvOrDefault("CONVERSATION_LOG_QUEUE_SIZE", 2048), 16), 8192))
var conversationSlots = make(chan struct{}, min(max(common.GetEnvOrDefault("CONVERSATION_LOG_MAX_INFLIGHT", 128), 8), 1024))
var conversationStart sync.Once
var conversationDropped atomic.Uint64
var conversationQueueBytes atomic.Int64
var conversationQueueLimit = int64(min(max(common.GetEnvOrDefault("CONVERSATION_LOG_QUEUE_MIB", 64), 4), 256)) << 20

// A durable admission/outcome is the normal path. The bounded queue is a second
// chance on database interruption, not the sole copy of every live request.
func persistConversation(item conversationWrite, finish bool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return model.RecordQA(ctx, item.entry, item.facts, model.ConversationCutoff(), finish)
}

func ConversationLoggingEnabled() bool { return os.Getenv("CONVERSATION_LOG_ENABLED") == "true" }

func StartConversationRecorder() {
	conversationStart.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for {
				select {
				case item := <-conversationQueue:
					if item.drained != nil {
						close(item.drained)
						continue
					}
					err := persistConversation(item, true, 2*time.Second)
					conversationQueueBytes.Add(-item.weight)
					if err != nil {
						conversationDropped.Add(1)
						common.SysError("conversation QA persistence failed; request_id=" + item.entry.RequestId)
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

// The acknowledgement is processed after preceding transactions commit.
func FlushConversations(timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	done := make(chan struct{})
	select {
	case conversationQueue <- conversationWrite{drained: done}:
	case <-timer.C:
		return
	}
	select {
	case <-done:
	case <-timer.C:
	}
}

type ConversationCapture struct {
	mu       sync.Mutex
	entry    model.QARequestMeta
	qa       qaCapture
	finished bool
	slot     bool
}

func BeginConversation(c *gin.Context, protocol string) *ConversationCapture {
	if !ConversationLoggingEnabled() || c.GetInt("id") <= 0 {
		return nil
	}
	slot := false
	select {
	case conversationSlots <- struct{}{}:
		slot = true
	default:
	}
	r := &ConversationCapture{slot: slot, entry: model.QARequestMeta{RequestId: c.GetString(common.RequestIdKey), UserId: c.GetInt("id"), TokenId: c.GetInt(string(constant.ContextKeyTokenId)), CreatedAt: time.Now().Unix()}, qa: qaCapture{facts: model.QAFacts{Missing: true, InputState: "unavailable"}, limit: min(max(common.GetEnvOrDefault("CONVERSATION_RESPONSE_SAFETY_BYTES", 16<<20), 1<<20), 64<<20), ws: protocol == "websocket"}}
	if r.entry.RequestId == "" {
		r.entry.RequestId = common.NewRequestId()
	}
	kind := "chat"
	if r.qa.ws || strings.HasSuffix(strings.TrimSuffix(c.Request.URL.Path, "/"), "/responses") {
		kind = "responses"
	}
	r.entry.Protocol = kind
	if r.qa.ws {
		r.entry.Protocol = "responses_ws"
	}
	r.qa.facts.Client = kind
	common.SysLog(fmt.Sprintf("QA ingress id=%s uid=%d protocol=%s at=%d", r.entry.RequestId, r.entry.UserId, r.entry.Protocol, r.entry.CreatedAt))
	if !slot {
		r.qa.facts.InputState = "capacity"
	} else if storage, err := common.GetBodyStorage(c); err == nil {
		limit := min(max(common.GetEnvOrDefault("CONVERSATION_LOG_PARSE_MAX_BYTES", 32<<20), 64<<10), 128<<20)
		body, readErr := io.ReadAll(io.LimitReader(storage, int64(limit)+1))
		_, seekErr := storage.Seek(0, io.SeekStart)
		c.Request.Body = io.NopCloser(storage)
		if len(body) > limit {
			r.qa.facts.InputState = "input_limit"
		} else if readErr == nil && seekErr == nil {
			var request map[string]any
			if common.Unmarshal(body, &request) == nil {
				r.qa.facts = qaRequest(request, c.Request.Header, c.Request.URL.Path, kind)
			}
		}
	}
	admissionAt := time.Now()
	if err := persistConversation(conversationWrite{entry: r.entry, facts: r.qa.facts}, false, 300*time.Millisecond); err != nil {
		common.SysError("QA admission delayed; id=" + r.entry.RequestId)
	}
	if elapsed := time.Since(admissionAt); elapsed >= 50*time.Millisecond {
		common.SysLog(fmt.Sprintf("QA slow admission id=%s ms=%d", r.entry.RequestId, elapsed.Milliseconds()))
	}
	c.Set(conversationContextKey, r)
	return r
}

func ConversationFromContext(c *gin.Context) *ConversationCapture {
	v, _ := c.Get(conversationContextKey)
	r, _ := v.(*ConversationCapture)
	return r
}

func (r *ConversationCapture) Observe(data []byte) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.finished && r.slot {
		r.qa.write(data)
	}
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
	if r.slot {
		defer func() { <-conversationSlots }()
	}
	r.qa.finishWire()
	r.qa.result(&r.qa.facts, status >= 200 && status < 300 && !cancelled)
	r.qa.facts.HTTPStatus = status
	if cancelled {
		r.qa.facts.OutputState = "cancelled"
	} else if !r.slot && status >= 200 && status < 300 {
		r.qa.facts.OutputState = "capacity"
	}
	item := conversationWrite{entry: r.entry, facts: r.qa.facts}
	if persistConversation(item, true, 400*time.Millisecond) == nil {
		return
	}
	common.SysError("QA outcome deferred; id=" + r.entry.RequestId)
	item.weight = int64(len(item.facts.Prompt) + len(item.facts.Final) + len(item.facts.Parts) + 1024 + 256*(len(item.facts.Returned)+len(item.facts.Issued)))
	if conversationQueueBytes.Add(item.weight) <= conversationQueueLimit {
		select {
		case conversationQueue <- item:
			return
		default:
		}
	}
	conversationQueueBytes.Add(-item.weight)
	conversationDropped.Add(1)
	common.SysError("QA outcome not persisted; id=" + r.entry.RequestId)
}
