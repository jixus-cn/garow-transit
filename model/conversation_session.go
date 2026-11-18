package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ConversationSession is the metadata skeleton of one reconstructed conversation.
// It never holds message bodies; those live append-only in ConversationMessage.
type ConversationSession struct {
	Id               int64  `json:"id" gorm:"primaryKey"`
	SessionKey       string `json:"session_key" gorm:"type:varchar(96);uniqueIndex"`
	ClientKey        string `json:"client_key" gorm:"type:varchar(255);index"`
	UserId           int    `json:"user_id" gorm:"index"`
	TokenId          int    `json:"token_id" gorm:"index"`
	TokenName        string `json:"token_name" gorm:"type:varchar(255)"`
	Source           string `json:"source" gorm:"type:varchar(16)"`
	ParentSessionId  int64  `json:"parent_session_id" gorm:"index"`
	LinkReason       string `json:"link_reason" gorm:"type:varchar(32)"`
	Model            string `json:"model" gorm:"type:varchar(255)"`
	LastModel        string `json:"last_model" gorm:"type:varchar(255)"`
	Protocol         string `json:"protocol" gorm:"type:varchar(32)"`
	CreatedAt        int64  `json:"created_at" gorm:"index"`
	UpdatedAt        int64  `json:"updated_at" gorm:"index"`
	MessageCount     int    `json:"message_count"`
	UserMessageCount int    `json:"user_message_count"`
	RequestCount     int    `json:"request_count"`
	LastOutcome      string `json:"last_outcome" gorm:"type:varchar(32)"`
}

// ConversationMessage is one visible utterance, stored exactly once per session.
// Rows are append-only; retention cleanup is the only deletion path.
type ConversationMessage struct {
	Id           int64  `json:"id" gorm:"primaryKey"`
	SessionId    int64  `json:"session_id" gorm:"index"`
	Role         string `json:"role" gorm:"type:varchar(16)"`
	Kind         string `json:"kind" gorm:"type:varchar(16)"`
	Content      string `json:"content"`
	Truncated    bool   `json:"truncated"`
	Interjection bool   `json:"interjection"`
	ContentHash  string `json:"content_hash" gorm:"type:varchar(32);index"`
	RequestId    string `json:"request_id" gorm:"type:varchar(64);index"`
	CreatedAt    int64  `json:"created_at" gorm:"index"`
}

// ConversationUserItem is one user-role input item as sent by the client.
// Hash covers the full text even when stored content is truncated.
type ConversationUserItem struct {
	Hash    string `json:"hash"`
	Text    string `json:"text"`
	Context bool   `json:"context"`
}

// ConversationExtraction carries request-side facts from capture to the worker.
type ConversationExtraction struct {
	ClientKey      string
	UserItems      []ConversationUserItem
	ItemCount      int
	PrevResponseId string
	Placeholder    bool
}

func ConversationHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:16])
}

func conversationSessionKey(prefix, seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return prefix + hex.EncodeToString(sum[:12])
}

// uniqueSessionKey makes a session key unique per creating request. Many IDE
// sessions share an identical first user item (injected environment context),
// so a seed-only key would collide; matching uses hash sequences, not the key.
func uniqueSessionKey(prefix, seed, requestId string) string {
	if len(requestId) > 64 {
		requestId = requestId[:64]
	}
	return conversationSessionKey(prefix, seed) + ":" + requestId
}

// conversationUserDelta compares hash sequences positionally. Identical texts at
// different positions stay distinct; only a tail extension yields new messages.
func conversationUserDelta(stored []string, items []ConversationUserItem) ([]ConversationUserItem, bool) {
	incoming := make([]string, len(items))
	for i, item := range items {
		incoming[i] = item.Hash
	}
	short, long := stored, incoming
	if len(long) < len(short) {
		short, long = long, short
	}
	for i := range short {
		if short[i] != long[i] {
			return nil, false
		}
	}
	if len(incoming) > len(stored) {
		return items[len(stored):], true
	}
	return nil, true
}

func sessionUserHashes(tx *gorm.DB, sessionId int64) ([]string, error) {
	var hashes []string
	err := tx.Model(&ConversationMessage{}).
		Where("session_id = ? AND role = ?", sessionId, "user").
		Order("created_at, id").Pluck("content_hash", &hashes).Error
	return hashes, err
}

func newConversationSession(entry *ConversationLog, key, clientKey, source string, parent int64, reason string) *ConversationSession {
	return &ConversationSession{
		SessionKey: key, ClientKey: clientKey, UserId: entry.UserId,
		TokenId: entry.TokenId, TokenName: entry.TokenName, Source: source,
		ParentSessionId: parent, LinkReason: reason,
		Model: entry.Model, LastModel: entry.Model, Protocol: entry.Protocol,
		CreatedAt: entry.CreatedAt, UpdatedAt: entry.CreatedAt, LastOutcome: entry.Outcome,
	}
}

// residualConversationSession is a deterministic per-user catch-all for turns
// whose request side is unrecoverable (oversized/placeholder) but whose assistant
// text survives. Cleaning keeps that text visible instead of dropping it; the
// source label tells the UI these fragments are not attributed to a conversation.
func residualConversationSession(tx *gorm.DB, entry *ConversationLog) (*ConversationSession, error) {
	key := conversationSessionKey("u:", fmt.Sprintf("%d", entry.UserId))
	session := newConversationSession(entry, key, "", "residual", 0, "")
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "session_key"}}, DoNothing: true}).Create(session).Error; err != nil {
		return nil, err
	}
	var found ConversationSession
	if err := tx.Where("session_key = ?", key).First(&found).Error; err != nil {
		return nil, err
	}
	return &found, nil
}

// resolveConversationSession joins a request to a session using only explicit
// identifiers or deterministic prefix chains, never text similarity or time
// proximity. A diverging history starts a child session; old rows stay intact.
func resolveConversationSession(tx *gorm.DB, entry *ConversationLog, ex *ConversationExtraction) (*ConversationSession, []ConversationUserItem, error) {
	var session *ConversationSession
	if ex.ClientKey != "" {
		var found ConversationSession
		err := tx.Where("user_id = ? AND client_key = ?", entry.UserId, ex.ClientKey).
			Order("updated_at DESC, id DESC").First(&found).Error
		if err == nil {
			session = &found
		} else if err != gorm.ErrRecordNotFound {
			return nil, nil, err
		}
		if session == nil {
			session = newConversationSession(entry, uniqueSessionKey("c:", ex.ClientKey, entry.RequestId), ex.ClientKey, "client", 0, "")
		}
	}
	if session == nil && ex.PrevResponseId != "" {
		var parent ConversationLog
		err := tx.Select("session_key").Where("user_id = ? AND response_id = ? AND session_state = ?", entry.UserId, ex.PrevResponseId, "linked").
			Order("created_at DESC, id DESC").First(&parent).Error
		if err == nil && parent.SessionKey != "" {
			var found ConversationSession
			if err = tx.Where("session_key = ?", parent.SessionKey).First(&found).Error; err == nil {
				session = &found
			} else if err != gorm.ErrRecordNotFound {
				return nil, nil, err
			}
		} else if err != gorm.ErrRecordNotFound {
			return nil, nil, err
		}
	}
	if session == nil && len(ex.UserItems) > 0 {
		var candidates []ConversationSession
		if err := tx.Where("user_id = ? AND token_id = ? AND source <> ?", entry.UserId, entry.TokenId, "residual").
			Order("updated_at DESC, id DESC").Limit(20).Find(&candidates).Error; err != nil {
			return nil, nil, err
		}
		best := -1
		bestLen := -1
		for i := range candidates {
			hashes, err := sessionUserHashes(tx, candidates[i].Id)
			if err != nil {
				return nil, nil, err
			}
			if _, ok := conversationUserDelta(hashes, ex.UserItems); ok && len(hashes) > bestLen {
				best, bestLen = i, len(hashes)
			}
		}
		if best >= 0 {
			session = &candidates[best]
		} else {
			seed := fmt.Sprintf("%d|%d|%s", entry.UserId, entry.TokenId, ex.UserItems[0].Hash)
			parent := int64(0)
			reason := ""
			for i := range candidates {
				hashes, err := sessionUserHashes(tx, candidates[i].Id)
				if err != nil {
					return nil, nil, err
				}
				if len(hashes) > 0 && hashes[0] == ex.UserItems[0].Hash {
					parent = candidates[i].Id
					reason = "compaction_or_fork"
					break
				}
			}
			session = newConversationSession(entry, uniqueSessionKey("p:", seed, entry.RequestId), "", "prefix", parent, reason)
		}
	}
	if session == nil {
		return nil, nil, nil
	}

	if session.Id == 0 {
		if err := tx.Create(session).Error; err != nil {
			return nil, nil, err
		}
		return session, ex.UserItems, nil
	}

	stored, err := sessionUserHashes(tx, session.Id)
	if err != nil {
		return nil, nil, err
	}
	newItems, compatible := conversationUserDelta(stored, ex.UserItems)
	if compatible {
		return session, newItems, nil
	}
	child := newConversationSession(entry, uniqueSessionKey("b:", fmt.Sprintf("%d", session.Id), entry.RequestId), session.ClientKey, session.Source, session.Id, "branch")
	if err := tx.Create(child).Error; err != nil {
		return nil, nil, err
	}
	return child, ex.UserItems, nil
}

// StoreSessionConversation appends only the delta of a request to its session.
// backfill updates an existing ledger row instead of creating one.
func StoreSessionConversation(entry *ConversationLog, ex *ConversationExtraction, assistantText string, toolCalls int, textMax int, legacy *ConversationContent, audit AuditLog, backfill bool) error {
	var appendedUsers int
	var appendedAssistant bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		session, newItems, err := resolveConversationSession(tx, entry, ex)
		if err != nil {
			return err
		}
		if session == nil && assistantText != "" {
			session, err = residualConversationSession(tx, entry)
			if err != nil {
				return err
			}
		}
		entry.NewUserMessages = len(newItems)
		entry.AssistantTextChars = len(assistantText)
		entry.ToolCalls = toolCalls
		if session == nil {
			entry.SessionState = "unassigned"
		} else {
			entry.SessionKey = session.SessionKey
			entry.SessionState = "linked"
			interjection := len(newItems) > 0 && (session.LastOutcome == "interrupted" || session.LastOutcome == "incomplete")
			for i, item := range newItems {
				content, truncated := truncateConversationText(item.Text, textMax)
				kind := "text"
				if item.Context {
					kind = "context"
				}
				msg := ConversationMessage{
					SessionId: session.Id, Role: "user", Kind: kind, Content: content,
					Truncated: truncated, Interjection: interjection && i == 0,
					ContentHash: item.Hash, RequestId: entry.RequestId, CreatedAt: entry.CreatedAt,
				}
				if err := tx.Create(&msg).Error; err != nil {
					return err
				}
				appendedUsers++
			}
			if assistantText != "" {
				var existing int64
				if err := tx.Model(&ConversationMessage{}).Where("request_id = ? AND role = ?", entry.RequestId, "assistant").Count(&existing).Error; err != nil {
					return err
				}
				if existing == 0 {
					content, truncated := truncateConversationText(assistantText, textMax)
					msg := ConversationMessage{
						SessionId: session.Id, Role: "assistant", Kind: "text", Content: content,
						Truncated: truncated, ContentHash: ConversationHash(assistantText),
						RequestId: entry.RequestId, CreatedAt: entry.CreatedAt,
					}
					if err := tx.Create(&msg).Error; err != nil {
						return err
					}
					appendedAssistant = true
				}
			}
			updates := map[string]any{
				"request_count":      session.RequestCount + 1,
				"message_count":      session.MessageCount + appendedUsers + boolToInt(appendedAssistant),
				"user_message_count": session.UserMessageCount + appendedUsers,
			}
			if entry.CreatedAt >= session.UpdatedAt {
				updates["updated_at"] = entry.CreatedAt
				updates["last_outcome"] = entry.Outcome
				if entry.Model != "" {
					updates["last_model"] = entry.Model
				}
			}
			if err := tx.Model(&ConversationSession{}).Where("id = ?", session.Id).Updates(updates).Error; err != nil {
				return err
			}
		}
		if backfill {
			return tx.Model(&ConversationLog{}).Where("request_id = ?", entry.RequestId).
				Updates(map[string]any{"session_key": entry.SessionKey, "session_state": entry.SessionState,
					"new_user_messages": entry.NewUserMessages, "assistant_text_chars": entry.AssistantTextChars,
					"tool_calls": entry.ToolCalls}).Error
		}
		if legacy != nil {
			encoded, err := common.Marshal(legacy)
			if err != nil {
				return err
			}
			entry.Payload = compressConversationPayload(encoded)
		}
		return tx.Create(entry).Error
	})
	if err != nil {
		return err
	}
	if !backfill {
		RecordAuditLog(nil, audit)
	}
	return nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func truncateConversationText(text string, max int) (string, bool) {
	if max <= 0 || len(text) <= max {
		return text, false
	}
	cut := max
	for cut > 0 && (text[cut]&0xC0) == 0x80 {
		cut--
	}
	return text[:cut], true
}

// PendingConversationBackfill selects legacy rows not yet mapped to a session.
// Migrated rows carry NULL session_state; freshly inserted rows carry ”.
func PendingConversationBackfill(limit int) ([]ConversationLog, error) {
	var rows []ConversationLog
	err := DB.Where("(session_state = ? OR session_state IS NULL) AND payload IS NOT NULL", "").
		Order("user_id, created_at, id").Limit(limit).Find(&rows).Error
	return rows, err
}

func GetConversationSessions(userId int, start, end int64, page, pageSize int) ([]ConversationSession, int64, error) {
	query := DB.Model(&ConversationSession{})
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if start > 0 {
		query = query.Where("updated_at >= ?", start)
	}
	if end > 0 {
		query = query.Where("updated_at <= ?", end)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var sessions []ConversationSession
	err := query.Order("updated_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&sessions).Error
	return sessions, total, err
}

// GetConversationSessionDetail returns one page of messages plus the request
// ledger for tool-activity dividers. Messages order by capture time, not append
// order, so backfilled history interleaves correctly.
func GetConversationSessionDetail(id int64, beforeId int64, limit int) (*ConversationSession, []ConversationMessage, []ConversationLog, bool, error) {
	var session ConversationSession
	if err := DB.First(&session, id).Error; err != nil {
		return nil, nil, nil, false, err
	}
	query := DB.Where("session_id = ?", id)
	if beforeId > 0 {
		query = query.Where("id < ?", beforeId)
	}
	var messages []ConversationMessage
	if err := query.Order("id DESC").Limit(limit + 1).Find(&messages).Error; err != nil {
		return nil, nil, nil, false, err
	}
	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	var ledger []ConversationLog
	if err := DB.Omit("payload").Where("session_key = ? AND session_state = ?", session.SessionKey, "linked").
		Order("created_at, id").Limit(1000).Find(&ledger).Error; err != nil {
		return nil, nil, nil, false, err
	}
	return &session, messages, ledger, hasMore, nil
}

func SearchConversationMessages(keyword string, userId int, start, end int64, limit int) ([]ConversationMessage, error) {
	query := DB.Model(&ConversationMessage{})
	if userId > 0 {
		query = query.Where("session_id IN (?)", DB.Model(&ConversationSession{}).Select("id").Where("user_id = ?", userId))
	}
	if start > 0 {
		query = query.Where("created_at >= ?", start)
	}
	if end > 0 {
		query = query.Where("created_at <= ?", end)
	}
	escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(keyword)
	var messages []ConversationMessage
	err := query.Where("content LIKE ? ESCAPE '!'", "%"+escaped+"%").
		Order("created_at DESC, id DESC").Limit(limit).Find(&messages).Error
	return messages, err
}

func GetConversationRequest(requestId string) (*ConversationLog, error) {
	var entry ConversationLog
	err := DB.Omit("payload").Where("request_id = ?", requestId).First(&entry).Error
	return &entry, err
}

// CleanupConversationSessions removes expired sessions and their messages.
func CleanupConversationSessions(cutoff int64) error {
	expired := DB.Model(&ConversationSession{}).Select("id").Where("updated_at < ?", cutoff)
	if err := DB.Where("session_id IN (?)", expired).Delete(&ConversationMessage{}).Error; err != nil {
		return err
	}
	return DB.Where("updated_at < ?", cutoff).Delete(&ConversationSession{}).Error
}

// ConversationRequestPreview extracts the human request from an IDE-wrapped
// user message (file-mention preamble + "## My request:") and truncates it for
// session list previews.
func ConversationRequestPreview(text string) string {
	if idx := strings.Index(text, "## My request:"); idx >= 0 {
		text = text[idx+len("## My request:"):]
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if utf8.RuneCountInString(text) > 80 {
		return string([]rune(text)[:80]) + "…"
	}
	return text
}

// ConversationSessionFirstUserText returns the first user text message content
// per session id, used to build list previews.
func ConversationSessionFirstUserText(ids []int64) map[int64]string {
	result := make(map[int64]string)
	if len(ids) == 0 {
		return result
	}
	sub := DB.Model(&ConversationMessage{}).Select("min(id)").
		Where("session_id IN ? AND role = ? AND kind = ?", ids, "user", "text").
		Group("session_id")
	var rows []ConversationMessage
	if err := DB.Where("id IN (?)", sub).Find(&rows).Error; err != nil {
		return result
	}
	for _, row := range rows {
		result[row.SessionId] = row.Content
	}
	return result
}

// ConversationUsernames maps user ids to usernames for session list display.
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
