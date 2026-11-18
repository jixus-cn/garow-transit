package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"slices"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A chat is a durable reference, not a request or a row created for each turn.
// Its list is derived from retained turns, so reopening a chat cannot revive expired text.
type QATurn struct {
	Id           int64   `json:"id" gorm:"primaryKey"`
	Scope        string  `json:"-" gorm:"size:64;index"`
	UserId       int     `json:"user_id" gorm:"index"`
	TokenId      int     `json:"-" `
	ChatKey      string  `json:"chat_key" gorm:"size:64;index:idx_qa_chat_time,priority:1"`
	ChatBasis    string  `json:"-" gorm:"size:12"`
	FirstRequest string  `json:"-" gorm:"size:64;uniqueIndex"`
	Submission   *string `json:"-" gorm:"size:64;uniqueIndex"`
	CreatedAt    int64   `json:"created_at" gorm:"index;index:idx_qa_chat_time,priority:2;autoCreateTime:false"`
	UpdatedAt    int64   `json:"-" gorm:"autoUpdateTime:false"`
	Prompt       string  `json:"prompt" gorm:"size:1048576"`
	Final        string  `json:"final" gorm:"size:1048576"`
	AnswerState  string  `json:"-" gorm:"size:16"`
}

func (QATurn) TableName() string { return "conversation_qa_turns" }

type QAReceipt struct {
	RequestId string `gorm:"size:64;primaryKey"`
	Scope     string `gorm:"size:64;index"`
	TurnId    int64  `gorm:"index"`
	CreatedAt int64  `gorm:"index;autoCreateTime:false"`
	State     string `gorm:"size:20;index"`
	Basis     string `gorm:"size:20"`
}

func (QAReceipt) TableName() string { return "conversation_qa_receipts" }

type QAEdge struct {
	Key        string `gorm:"column:edge_hash;size:64;primaryKey"`
	RequestId  string `gorm:"size:64;primaryKey"`
	TurnId     int64  `gorm:"index"`
	CreatedAt  int64  `gorm:"index;autoCreateTime:false"`
	Tool       bool   `gorm:"index"`
	ConsumedBy string `gorm:"size:64"`
}

func (QAEdge) TableName() string { return "conversation_qa_edges" }

type QAFacts struct {
	Client, Chat, Submission, Parent, Response, Prompt, Final string
	Returned, Issued                                          []string
	Unclear, Missing                                          bool
}

func qaDigest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:", len(p))
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

var qaWriter sync.Mutex // Garow has one production writer; SQL uniqueness also makes retries idempotent.

// StoreQA atomically projects one observed request. Edges carry only scoped digests;
// they may attach a reply to an earlier turn but cannot invent a user submission.
func StoreQA(entry QARequestMeta, f QAFacts, cutoff int64) error {
	qaWriter.Lock()
	defer qaWriter.Unlock()
	return DB.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		var done int64
		if err := tx.Model(&QAReceipt{}).Where("request_id = ?", entry.RequestId).Count(&done).Error; err != nil || done != 0 {
			return err
		}
		scope := qaDigest(fmt.Sprint(entry.UserId), fmt.Sprint(entry.TokenId), f.Client)
		chat := ""
		if f.Chat != "" {
			chat = qaDigest(scope, "chat", f.Chat)
		}
		r := QAReceipt{RequestId: entry.RequestId, Scope: scope, CreatedAt: entry.CreatedAt, State: "no_turn"}
		var parent, turn, submitted QATurn
		var sub *string
		if f.Submission != "" {
			digest := qaDigest(scope, chat, "submission", f.Submission)
			sub = &digest
			err := tx.Where("submission = ?", digest).Take(&submitted).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		var refs []QAEdge
		conflict, missing := false, false
		inputs := make([]string, 0, len(f.Returned)+1)
		if f.Parent != "" {
			inputs = append(inputs, qaDigest(scope, "response", f.Parent))
		}
		for _, v := range f.Returned {
			inputs = append(inputs, qaDigest(scope, "tool", v))
		}
		for _, k := range inputs {
			var edges []QAEdge
			if err := tx.Where("edge_hash = ? AND created_at >= ?", k, cutoff).Limit(2).Find(&edges).Error; err != nil {
				return err
			}
			if len(edges) == 0 {
				missing = true
				continue
			}
			if len(edges) != 1 {
				conflict = true
				continue
			}
			edge := edges[0]
			if len(refs) > 0 && refs[0].TurnId != edge.TurnId {
				conflict = true
			}
			refs = append(refs, edge)
		}
		if len(refs) > 0 && !conflict && !missing {
			e := tx.Where("id = ? AND scope = ? AND created_at >= ?", refs[0].TurnId, scope, cutoff).Take(&parent).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				missing = true
			} else if e != nil {
				return e
			}
			if chat != "" && parent.ChatBasis == "client" && parent.ChatKey != chat {
				conflict = true
			}
		}
		if f.Prompt != "" && !f.Unclear && entry.CreatedAt >= cutoff {
			turn = submitted
			if turn.Id != 0 && (turn.Prompt != f.Prompt || turn.CreatedAt < cutoff) {
				r.State = "ambiguous"
				return tx.Create(&r).Error
			}
			if turn.Id == 0 {
				turn = QATurn{Scope: scope, UserId: entry.UserId, TokenId: entry.TokenId, ChatKey: chat, FirstRequest: entry.RequestId, Submission: sub, Prompt: f.Prompt, CreatedAt: entry.CreatedAt, UpdatedAt: entry.CreatedAt, AnswerState: "pending"}
				if chat != "" {
					turn.ChatBasis = "client"
				}
				if parent.Id != 0 && !conflict && !missing {
					if chat == "" {
						turn.ChatKey, turn.ChatBasis = parent.ChatKey, parent.ChatBasis
						if turn.ChatKey == "" {
							turn.ChatKey, turn.ChatBasis = qaDigest(scope, "chain", parent.FirstRequest), "parent"
						}
					}
					if err := assignQAChat(tx, parent, turn.ChatKey, turn.ChatBasis); err != nil {
						return err
					}
				}
				if err := tx.Create(&turn).Error; err != nil {
					return err
				}
				r.Basis = "direct"
			} else {
				r.Basis = "submission"
			}
		} else if submitted.Id != 0 && len(inputs) == 0 && !f.Unclear && !f.Missing && submitted.CreatedAt >= cutoff {
			turn = submitted
			r.Basis = "submission"
		} else if parent.Id != 0 && (submitted.Id == 0 || submitted.Id == parent.Id) && !conflict && !missing && !f.Unclear && !f.Missing {
			turn = parent
			r.Basis = "parent"
			if len(f.Returned) > 0 {
				r.Basis = "tool"
			}
			// A tool result belongs only to the request that consumes it. A competing
			// continuation with a different final invalidates the pair, never overwrites it.
			for _, e := range refs {
				if e.Tool && e.ConsumedBy != "" && e.ConsumedBy != entry.RequestId {
					conflict = true
				}
			}
			if conflict {
				if f.Final != "" && turn.AnswerState == "answered" && turn.Final != f.Final {
					if err := tx.Model(&turn).Updates(map[string]any{"final": "", "answer_state": "ambiguous"}).Error; err != nil {
						return err
					}
				}
				r.State = "ambiguous"
				return tx.Create(&r).Error
			}
			if chat != "" {
				if err := assignQAChat(tx, turn, chat, "client"); err != nil {
					return err
				}
			}
			for _, e := range refs {
				if !e.Tool {
					continue
				}
				if err := tx.Model(&QAEdge{}).Where("edge_hash = ? AND request_id = ?", e.Key, e.RequestId).Update("consumed_by", entry.RequestId).Error; err != nil {
					return err
				}
			}
		}
		if turn.Id == 0 {
			switch {
			case conflict || f.Unclear:
				r.State = "ambiguous"
			case f.Final != "":
				r.State = "orphan_final"
			case f.Missing:
				r.State = "unavailable"
			}
		} else {
			r.State, r.TurnId = "linked", turn.Id
			updates := map[string]any{"updated_at": max(entry.CreatedAt, turn.UpdatedAt)}
			var pending int64
			if f.Final != "" {
				if err := tx.Model(&QAEdge{}).Where("turn_id = ? AND tool = ? AND consumed_by = ?", turn.Id, true, "").Count(&pending).Error; err != nil {
					return err
				}
			}
			if f.Final != "" && pending == 0 && turn.AnswerState != "ambiguous" {
				if turn.Final != "" && turn.Final != f.Final {
					updates["final"], updates["answer_state"] = "", "ambiguous"
				} else {
					updates["final"], updates["answer_state"] = f.Final, "answered"
				}
			}
			if err := tx.Model(&turn).Updates(updates).Error; err != nil {
				return err
			}
			issued := make([]string, 0, len(f.Issued)+1)
			responseKey := qaDigest(scope, "response", f.Response)
			if f.Response != "" {
				issued = append(issued, responseKey)
			}
			for _, v := range f.Issued {
				k := qaDigest(scope, "tool", v)
				if !slices.Contains(issued, k) {
					issued = append(issued, k)
				}
			}
			for _, k := range issued {
				if err := tx.Create(&QAEdge{Key: k, Tool: k != responseKey, RequestId: entry.RequestId, TurnId: turn.Id, CreatedAt: entry.CreatedAt}).Error; err != nil {
					return err
				}
			}
		}
		return tx.Create(&r).Error
	})
}

func assignQAChat(tx *gorm.DB, t QATurn, key, basis string) error {
	if key == "" || t.ChatKey == key {
		return nil
	}
	q := tx.Model(&QATurn{}).Where("scope = ?", t.Scope)
	if t.ChatKey == "" {
		q = q.Where("id = ?", t.Id)
	} else if t.ChatBasis == "parent" && basis == "client" {
		q = q.Where("chat_key = ?", t.ChatKey)
	} else {
		return nil
	}
	return q.Updates(map[string]any{"chat_key": key, "chat_basis": basis}).Error
}

type QAChatRow struct {
	ChatKey   string `json:"chat_key"`
	UserId    int    `json:"user_id"`
	Username  string `json:"username" gorm:"-"`
	Preview   string `json:"preview" gorm:"-"`
	TurnCount int64  `json:"turn_count"`
	UpdatedAt int64  `json:"updated_at"`
	FirstId   int64  `json:"-"`
}
type QAFilter struct {
	User               int
	Start, End, Cutoff int64
	Query              string
}

func qaFiltered(f QAFilter) *gorm.DB {
	q := DB.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Model(&QATurn{}).Where("created_at >= ?", max(f.Start, f.Cutoff))
	if f.End > 0 {
		q = q.Where("created_at <= ?", f.End)
	}
	if f.User > 0 {
		q = q.Where("user_id = ?", f.User)
	}
	if f.Query != "" {
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.Query)
		q = q.Where("(LOWER(prompt) LIKE LOWER(?) ESCAPE '!' OR LOWER(final) LIKE LOWER(?) ESCAPE '!')", "%"+escaped+"%", "%"+escaped+"%")
	}
	return q
}
func QAChats(f QAFilter, page, size int) ([]QAChatRow, int64, error) {
	group := func() *gorm.DB {
		return qaFiltered(f).Where("chat_key <> ''").Select("chat_key,user_id,MIN(id) AS first_id,MAX(updated_at) AS updated_at,COUNT(*) AS turn_count").Group("chat_key,user_id")
	}
	var total int64
	var rows []QAChatRow
	if err := DB.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Table("(?) AS chats", group()).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := group().Order("updated_at DESC, chat_key").Offset((page - 1) * size).Limit(size).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	var ids []int64
	var users []int
	for _, row := range rows {
		ids = append(ids, row.FirstId)
		users = append(users, row.UserId)
	}
	if len(ids) > 0 {
		var ts []QATurn
		if err := DB.Select("id,prompt").Where("id IN ?", ids).Find(&ts).Error; err != nil {
			return nil, 0, err
		}
		previews := map[int64]string{}
		for _, t := range ts {
			r := []rune(t.Prompt)
			previews[t.Id] = string(r[:min(len(r), 80)])
		}
		names := ConversationUsernames(users)
		for i := range rows {
			rows[i].Preview = previews[rows[i].FirstId]
			rows[i].Username = names[rows[i].UserId]
		}
	}
	return rows, total, nil
}
func QATurns(f QAFilter, key string, page, size int) ([]QATurn, int64, error) {
	q := qaFiltered(f)
	if key != "*" {
		q = q.Where("chat_key = ?", key)
	}
	var n int64
	var rows []QATurn
	if err := q.Count(&n).Error; err != nil {
		return nil, 0, err
	}
	order := "created_at, id"
	if key == "" || key == "*" {
		order = "created_at DESC, id DESC"
	}
	err := q.Order(order).Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, n, err
}
func QAByRequest(id string, cutoff int64) (*QATurn, error) {
	var r QAReceipt
	if err := DB.Where("request_id = ? AND created_at >= ?", id, cutoff).Take(&r).Error; err != nil {
		return nil, err
	}
	if r.TurnId == 0 {
		return nil, nil
	}
	var t QATurn
	err := DB.Where("id = ? AND scope = ? AND created_at >= ?", r.TurnId, r.Scope, cutoff).Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &t, err
}

func ConversationUsernames(ids []int) map[int]string {
	result := make(map[int]string)
	if len(ids) == 0 {
		return result
	}
	var users []User
	if err := DB.Select("id", "username").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return result
	}
	for _, user := range users {
		result[user.Id] = user.Username
	}
	return result
}

func ConversationCutoff() int64 {
	days := common.GetEnvOrDefault("CONVERSATION_RETENTION_DAYS", 0)
	if days == 0 {
		days = common.GetEnvOrDefault("CONVERSATION_LOG_RETENTION_DAYS", 30)
	}
	days = min(max(days, 1), 90)
	return time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
}

type QARequestMeta struct {
	RequestId       string
	UserId, TokenId int
	CreatedAt       int64
}

// Expire references to an old turn even when the continuation itself is newer.
func CleanupConversations(cutoff int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		expired := tx.Model(&QATurn{}).Select("id").Where("created_at < ?", cutoff)
		if err := tx.Model(&QAReceipt{}).Where("turn_id IN (?)", expired).Updates(map[string]any{"turn_id": 0, "state": "expired"}).Error; err != nil {
			return err
		}
		if err := tx.Where("turn_id IN (?)", expired).Delete(&QAEdge{}).Error; err != nil {
			return err
		}
		for _, table := range []any{&QAEdge{}, &QAReceipt{}, &QATurn{}} {
			if err := tx.Where("created_at < ?", cutoff).Delete(table).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
