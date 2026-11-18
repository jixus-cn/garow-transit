package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"sync"
)

// ConversationSession is the metadata skeleton of one reconstructed conversation.
// It never holds message bodies; those live append-only in ConversationMessage.
type ConversationSession struct {
	AnchorHash       string `json:"-" gorm:"type:varchar(32);index"`
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
	CreatedAt        int64  `json:"created_at" gorm:"index;autoCreateTime:false"`
	UpdatedAt        int64  `json:"updated_at" gorm:"index;autoUpdateTime:false"`
	MessageCount     int    `json:"message_count"`
	UserMessageCount int    `json:"user_message_count"`
	RequestCount     int    `json:"request_count"`
	LastOutcome      string `json:"last_outcome" gorm:"type:varchar(32)"`
}

// ConversationMessage is one visible utterance, stored exactly once per session.
// Rows are append-only; retention cleanup is the only deletion path.
type ConversationMessage struct {
	Origin       string `json:"origin" gorm:"type:varchar(16)"`
	Item         int    `json:"item_index"`
	Part         int    `json:"part_index"`
	Id           int64  `json:"id" gorm:"primaryKey"`
	SessionId    int64  `json:"session_id" gorm:"index"`
	Role         string `json:"role" gorm:"type:varchar(16)"`
	Kind         string `json:"kind" gorm:"type:varchar(16)"`
	Content      string `json:"content"`
	Truncated    bool   `json:"truncated"`
	Interjection bool   `json:"interjection"`
	ContentHash  string `json:"content_hash" gorm:"type:varchar(32);index"`
	RequestId    string `json:"request_id" gorm:"type:varchar(64);index"`
	CreatedAt    int64  `json:"created_at" gorm:"index;autoCreateTime:false"`
}

// ConversationUserItem is one user-role input item as sent by the client.
// Hash covers the full text even when stored content is truncated.
type ConversationUserItem struct {
	Hash      string `json:"-"`
	Text      string `json:"text"`
	Context   bool   `json:"-"`
	Role      string `json:"role"`
	Kind      string `json:"kind"`
	Origin    string `json:"origin"`
	Item      int    `json:"item"`
	Part      int    `json:"part"`
	Truncated bool   `json:"truncated,omitempty"`
}

func (p ConversationUserItem) IsQuestion() bool {
	return (p.Role == "" || p.Role == "user") && !p.Context && (p.Kind == "" || p.Kind == "text") && p.Text != ""
}

func (ConversationSession) TableName() string { return "conversation_sessions_v2" }
func (ConversationMessage) TableName() string { return "conversation_messages_v2" }

// ConversationExtraction carries request-side facts from capture to the worker.
type ConversationExtraction struct {
	Outputs        []ConversationUserItem
	Snapshot       bool
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

// Match authored request segments only. Client context and reported replies
// are evidence, but neither establishes conversation identity.
func questionSequence(items []ConversationUserItem) []string {
	var hashes []string
	for _, v := range items {
		if v.IsQuestion() {
			hashes = append(hashes, v.Hash)
		}
	}
	return hashes
}
func extendsQuestions(incoming, stored []string) bool {
	return len(stored) > 0 && len(incoming) >= len(stored) && slices.Equal(stored, incoming[:len(stored)])
}
func afterQuestions(items []ConversationUserItem, n int) []ConversationUserItem {
	if n == 0 {
		return items
	}
	for i, v := range items {
		if v.IsQuestion() {
			n--
			if n == 0 {
				return items[i+1:]
			}
		}
	}
	return nil
}
func sessionUserHashes(tx *gorm.DB, sessionId int64) ([]string, error) {
	var hashes []string
	err := tx.Model(&ConversationMessage{}).Where("session_id = ? AND role = ? AND kind = ?", sessionId, "user", "text").Order("id").Pluck("content_hash", &hashes).Error
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

// Resolve a protocol parent within the same principal, then a typed client
// conversation ID. An unlinked snapshot may extend exactly one substantive
// prefix; a shared preamble, cache key, or account ID is never a conversation.
func resolveConversationSession(tx *gorm.DB, entry *ConversationLog, ex *ConversationExtraction) (*ConversationSession, []ConversationUserItem, error) {
	q := questionSequence(ex.UserItems)
	anchor := ""
	if len(q) > 0 {
		anchor = q[0]
	}
	create := func(source string, parent int64, reason string) (*ConversationSession, []ConversationUserItem, error) {
		s := newConversationSession(entry, uniqueSessionKey("v2:", fmt.Sprintf("%d|%d", entry.UserId, entry.TokenId), entry.RequestId), ex.ClientKey, source, parent, reason)
		s.AnchorHash = anchor
		if err := tx.Create(s).Error; err != nil {
			return nil, nil, err
		}
		return s, ex.UserItems, nil
	}
	var existing *ConversationSession
	if ex.PrevResponseId != "" {
		var parents []ConversationLog
		if err := tx.Select("replay_key").Where("user_id = ? AND token_id = ? AND response_id = ? AND replay_state = ?", entry.UserId, entry.TokenId, ex.PrevResponseId, "linked").Limit(2).Find(&parents).Error; err != nil {
			return nil, nil, err
		}
		if len(parents) != 1 {
			return create("fragment", 0, "unresolved_parent")
		}
		var s ConversationSession
		if err := tx.Where("session_key = ?", parents[0].SessionKey).Take(&s).Error; err != nil {
			return nil, nil, err
		}
		if ex.ClientKey != "" && s.ClientKey != "" && ex.ClientKey != s.ClientKey {
			return create("fragment", 0, "conflicting_ids")
		}
		var siblings int64
		if err := tx.Model(&ConversationLog{}).Where("user_id = ? AND token_id = ? AND previous_response_id = ? AND replay_state = ?", entry.UserId, entry.TokenId, ex.PrevResponseId, "linked").Count(&siblings).Error; err != nil {
			return nil, nil, err
		}
		if siblings > 0 {
			return create("responses", s.Id, "branch")
		}
		if s.Source != "client" && s.Source != "responses" {
			if err := tx.Model(&s).Update("source", "responses").Error; err != nil {
				return nil, nil, err
			}
		}
		existing = &s
	} else if ex.ClientKey != "" {
		var matches []ConversationSession
		if err := tx.Where("user_id = ? AND token_id = ? AND client_key = ?", entry.UserId, entry.TokenId, ex.ClientKey).Limit(2).Find(&matches).Error; err != nil {
			return nil, nil, err
		}
		if len(matches) == 0 {
			return create("client", 0, "")
		}
		if len(matches) > 1 {
			return create("fragment", 0, "ambiguous_client")
		}
		existing = &matches[0]
	}
	if existing != nil {
		if ex.PrevResponseId != "" && existing.ClientKey == "" && ex.ClientKey != "" {
			if err := tx.Model(existing).Update("client_key", ex.ClientKey).Error; err != nil {
				return nil, nil, err
			}
		}
		// With a protocol parent, one new question is an increment, even if its text repeats.
		if !ex.Snapshot && len(q) <= 1 {
			return existing, ex.UserItems, nil
		}
		old, err := sessionUserHashes(tx, existing.Id)
		if err != nil {
			return nil, nil, err
		}
		if len(old) == 0 || len(q) == 0 {
			return existing, ex.UserItems, nil
		}
		if extendsQuestions(q, old) {
			return existing, afterQuestions(ex.UserItems, len(old)), nil
		}
		// A tool-result/history fragment does not invalidate an explicit parent.
		if ex.PrevResponseId != "" {
			var delta []ConversationUserItem
			for _, v := range ex.UserItems {
				if v.Origin != "snapshot" {
					delta = append(delta, v)
				}
			}
			return existing, delta, nil
		}
		return create("client", existing.Id, "branch")
	}
	if anchor == "" {
		if len(ex.UserItems) > 0 || len(ex.Outputs) > 0 {
			return create("residual", 0, "")
		}
		return nil, nil, nil
	}
	if len(q) > 1 || ex.Snapshot {
		var candidates []ConversationSession
		if err := tx.Where("user_id = ? AND token_id = ? AND anchor_hash = ? AND source IN ?", entry.UserId, entry.TokenId, anchor, []string{"prefix", "snapshot"}).Limit(21).Find(&candidates).Error; err != nil {
			return nil, nil, err
		}
		if len(candidates) > 20 {
			return create("fragment", 0, "ambiguous_prefix")
		}
		var match *ConversationSession
		var n int
		for i := range candidates {
			old, err := sessionUserHashes(tx, candidates[i].Id)
			if err != nil {
				return nil, nil, err
			}
			if extendsQuestions(q, old) {
				if match != nil {
					return create("fragment", 0, "ambiguous_prefix")
				}
				match = &candidates[i]
				n = len(old)
			}
		}
		if match != nil {
			if match.Source == "snapshot" {
				if err := tx.Model(match).Update("source", "prefix").Error; err != nil {
					return nil, nil, err
				}
			}
			return match, afterQuestions(ex.UserItems, n), nil
		}
	}
	return create("snapshot", 0, "")
}

var conversationProjectionMu sync.Mutex

// StoreSessionConversation appends only the delta of a request to its session.
// backfill updates an existing ledger row instead of creating one.
func StoreSessionConversation(entry *ConversationLog, ex *ConversationExtraction, assistantText string, toolCalls int, textMax int, legacy *ConversationContent, audit AuditLog, backfill bool) error {
	// Serialize live capture and backfill while resolving identity on this writer.
	conversationProjectionMu.Lock()
	defer conversationProjectionMu.Unlock()
	if len(ex.Outputs) == 0 && assistantText != "" {
		ex.Outputs = []ConversationUserItem{{Role: "assistant", Kind: "text", Origin: "response", Text: assistantText}}
	}
	err := DB.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}).Transaction(func(tx *gorm.DB) error {
		if backfill {
			var done int64
			if err := tx.Model(&ConversationLog{}).Where("request_id = ? AND replay_state IN ?", entry.RequestId, []string{"linked", "unassigned"}).Count(&done).Error; err != nil {
				return err
			}
			if done > 0 {
				return nil
			}
		}
		session, newItems, err := resolveConversationSession(tx, entry, ex)
		if err != nil {
			return err
		}
		entry.NewUserMessages = 0
		entry.AssistantTextChars = 0
		entry.ToolCalls = toolCalls
		entry.SessionKey = ""
		entry.SessionState = "unassigned"
		if session != nil {
			entry.SessionKey = session.SessionKey
			entry.SessionState = "linked"
			interjection := session.RequestCount > 0 && (session.LastOutcome == "interrupted" || session.LastOutcome == "incomplete")
			added := 0
			humanCount := 0
			seenQuestionItem := map[int]bool{}
			for _, item := range append(slices.Clone(newItems), ex.Outputs...) {
				if item.Text == "" {
					continue
				}
				if item.Kind == "" {
					item.Kind = "text"
					if item.Context {
						item.Kind = "context"
					}
				}
				if item.Role == "" {
					item.Role = "user"
				}
				indexed := item.Origin != ""
				if item.Origin == "" {
					item.Origin = "request"
				}
				if item.Hash == "" {
					item.Hash = ConversationHash(item.Text)
				}
				if session.RequestCount > 0 && (item.Origin == "snapshot" && item.Role == "assistant" || item.Kind == "context" || item.Kind == "unknown" && item.Role == "user") {
					var seen int64
					if err := tx.Model(&ConversationMessage{}).Where("session_id = ? AND role = ? AND content_hash = ?", session.Id, item.Role, item.Hash).Count(&seen).Error; err != nil {
						return err
					}
					if seen > 0 {
						continue
					}
				}
				text, cut := truncateConversationText(item.Text, textMax)
				msg := ConversationMessage{SessionId: session.Id, Role: item.Role, Kind: item.Kind, Origin: item.Origin, Item: item.Item, Part: item.Part, Content: text, Truncated: cut || item.Truncated, ContentHash: item.Hash, RequestId: entry.RequestId, CreatedAt: entry.CreatedAt}
				if item.IsQuestion() && (!indexed || !seenQuestionItem[item.Item]) {
					seenQuestionItem[item.Item] = true
					humanCount++
					if item.Origin == "request" {
						entry.NewUserMessages++
					}
					if item.Origin == "request" && interjection {
						msg.Interjection = true
						interjection = false
					}
				}
				if item.Role == "assistant" && item.Origin == "response" {
					entry.AssistantTextChars += utf8.RuneCountInString(item.Text)
				}
				if err := tx.Create(&msg).Error; err != nil {
					return err
				}
				added++
			}
			updates := map[string]any{"request_count": session.RequestCount + 1, "message_count": session.MessageCount + added, "user_message_count": session.UserMessageCount + humanCount}
			if entry.CreatedAt >= session.UpdatedAt {
				updates["updated_at"] = entry.CreatedAt
				updates["last_outcome"] = entry.Outcome
				if entry.Model != "" {
					updates["last_model"] = entry.Model
				}
			}
			if err := tx.Model(session).Updates(updates).Error; err != nil {
				return err
			}
		}
		if backfill {
			return tx.Model(&ConversationLog{}).Where("request_id = ?", entry.RequestId).
				Updates(map[string]any{"replay_key": entry.SessionKey, "replay_state": entry.SessionState,
					"replay_new_user_messages": entry.NewUserMessages, "replay_assistant_text_chars": entry.AssistantTextChars, "replay_tool_calls": entry.ToolCalls}).Error
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
	err := DB.Omit("payload").Where("(replay_state = ? OR replay_state IS NULL)", "").
		Order("user_id, created_at, id").Limit(limit).Find(&rows).Error
	return rows, err
}

func GetConversationSessions(userId int, start, end int64, page, pageSize int, includeFragments bool) ([]ConversationSession, int64, error) {
	query := DB.Model(&ConversationSession{})
	if !includeFragments {
		query = query.Where("source NOT IN ?", []string{"fragment", "residual"})
	}
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
	if err := DB.Omit("payload").Where("replay_key = ? AND replay_state = ?", session.SessionKey, "linked").
		Order("created_at, id").Find(&ledger).Error; err != nil {
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
	err := query.Where("kind IN ? AND content LIKE ? ESCAPE '!'", []string{"text", "final", "commentary", "partial"}, "%"+escaped+"%").
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
	if err := DB.Where("updated_at < ?", cutoff).Delete(&ConversationSession{}).Error; err != nil {
		return err
	}
	// Rollback tables retain their original expiry; a side-by-side upgrade must
	// not prolong retention of evidence produced by the previous version.
	if DB.Migrator().HasTable("conversation_sessions") && DB.Migrator().HasTable("conversation_messages") {
		old := DB.Table("conversation_sessions").Select("id").Where("updated_at < ?", cutoff)
		if err := DB.Table("conversation_messages").Where("session_id IN (?)", old).Delete(&ConversationMessage{}).Error; err != nil {
			return err
		}
		return DB.Table("conversation_sessions").Where("updated_at < ?", cutoff).Delete(&ConversationSession{}).Error
	}
	return nil
}

// ConversationRequestPreview extracts the human request from an IDE-wrapped
// user message (file-mention preamble + "## My request:") and truncates it for
// session list previews.
func ConversationRequestPreview(text string) string {
	var visible []string
	for _, v := range ConversationInputParts(text) {
		if v.IsQuestion() {
			visible = append(visible, v.Text)
		}
	}
	text = strings.Join(visible, "\n")
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
		Where("session_id IN ? AND role = ? AND kind = ? AND (origin = ? OR origin IS NULL OR origin = ?)", ids, "user", "text", "request", "").
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
