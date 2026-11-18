package service

import (
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
}

var conversationQueue = make(chan conversationWrite, min(max(common.GetEnvOrDefault("CONVERSATION_LOG_QUEUE_SIZE", 256), 16), 4096))
var conversationSlots = make(chan struct{}, min(max(common.GetEnvOrDefault("CONVERSATION_LOG_MAX_INFLIGHT", 128), 8), 1024))
var conversationStart sync.Once
var conversationDropped atomic.Uint64

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
					if err := model.StoreQA(item.entry, item.facts, model.ConversationCutoff()); err != nil {
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
	r := &ConversationCapture{
		entry: model.QARequestMeta{RequestId: c.GetString(common.RequestIdKey), UserId: c.GetInt("id"), TokenId: c.GetInt(string(constant.ContextKeyTokenId)), CreatedAt: time.Now().Unix()},
		qa:    qaCapture{facts: model.QAFacts{Missing: true}, limit: min(max(common.GetEnvOrDefault("CONVERSATION_RESPONSE_SAFETY_BYTES", 16<<20), 1<<20), 64<<20), ws: protocol == "websocket"},
	}
	if r.entry.RequestId == "" {
		r.entry.RequestId = common.NewRequestId()
	}
	kind := "chat"
	if r.qa.ws || strings.HasSuffix(strings.TrimSuffix(c.Request.URL.Path, "/"), "/responses") {
		kind = "responses"
	}
	if storage, err := common.GetBodyStorage(c); err == nil {
		limit := min(max(common.GetEnvOrDefault("CONVERSATION_LOG_PARSE_MAX_BYTES", 32<<20), 64<<10), 128<<20)
		body, readErr := io.ReadAll(io.LimitReader(storage, int64(limit)+1))
		_, seekErr := storage.Seek(0, io.SeekStart)
		c.Request.Body = io.NopCloser(storage)
		if readErr == nil && seekErr == nil && len(body) <= limit {
			var request map[string]any
			if common.Unmarshal(body, &request) == nil {
				r.qa.facts = qaRequest(request, c.Request.Header, c.Request.URL.Path, kind)
			}
		}
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
	if !r.finished {
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
	defer func() { <-conversationSlots }()
	r.qa.finishWire()
	r.qa.result(&r.qa.facts, status >= 200 && status < 300 && !cancelled)
	select {
	case conversationQueue <- conversationWrite{entry: r.entry, facts: r.qa.facts}:
	default:
		conversationDropped.Add(1)
	}
}
