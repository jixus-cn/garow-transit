package service

import (
	"errors"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

var qaBackfill sync.Mutex

func historicalQA(e *model.ConversationLog, c *model.ConversationContent) model.QAFacts {
	var req map[string]any
	_ = common.UnmarshalJsonStr(c.Request, &req)
	f := qaRequest(req, nil, "", e.Kind)
	if f.Chat == "" && c.ClientReference != "" {
		f.Chat = c.ClientReference

	}
	if f.Parent == "" {
		f.Parent = e.PreviousResponseId
	}
	var wire any
	_ = common.UnmarshalJsonStr(c.Response, &wire)
	var q qaCapture
	switch v := wire.(type) {
	case map[string]any:
		q.event(v)
	case []any:
		for _, raw := range v {
			if ev, ok := raw.(map[string]any); ok {
				q.event(ev)
			} else {
				q.invalid = true
			}
		}
	}
	good := e.Outcome == "completed" && !e.Truncated
	q.result(&f, good)
	// A v2 span is usable only with independent protocol termination, a single
	// final item and exact byte coverage. Never promote the old aggregate output.
	if good && q.terminal && q.sparse && !q.invalid && !q.failed && !q.hasTool && f.Final == "" && len(c.OutputSpans) > 0 {
		explicit := -1
		pos := 0
		var answer string
		for i, s := range c.OutputSpans {
			if i > 0 {
				if pos >= len(c.Output) || c.Output[pos] != '\n' {
					return f
				}
				pos++
			}
			if s.Bytes <= 0 || s.Bytes > len(c.Output)-pos {
				return f
			}
			if s.Kind == "final" {
				if explicit >= 0 && explicit != s.Item {
					return f
				}
				explicit = s.Item
				answer += c.Output[pos : pos+s.Bytes]
			} else if s.Kind != "commentary" {
				return f
			}
			pos += s.Bytes
		}
		if pos == len(c.Output) && len(answer) <= 1<<20 {
			f.Final = answer
		}
	}
	return f
}

// Receipts, not a moving high-water mark, are the durable checkpoint. Each
// batch reads only small metadata; only one compressed legacy body is decoded at once.
func BackfillQA() (int, error) {
	qaBackfill.Lock()
	defer qaBackfill.Unlock()
	cutoff := model.ConversationCutoff()
	count := 0
	for {
		var rows []model.ConversationLog
		err := model.DB.Select("id,request_id,user_id,token_id,created_at,kind").Where("created_at >= ? AND NOT EXISTS (SELECT 1 FROM conversation_qa_receipts r WHERE r.request_id = conversation_logs.request_id)", cutoff).Order("created_at,id").Limit(100).Find(&rows).Error
		if err != nil || len(rows) == 0 {
			return count, err
		}
		for i := range rows {
			e, c, err := model.GetConversation(rows[i].RequestId, cutoff)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			facts := model.QAFacts{Missing: true}
			if err == nil {
				facts = historicalQA(e, c)
			} else if e == nil {
				return count, err
			}
			if err := model.StoreQA(&rows[i], facts, true, model.AuditLog{}, cutoff); err != nil {
				return count, err
			}
			count++
		}
	}
}
