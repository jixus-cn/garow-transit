package model

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"io"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// ConversationLog is deliberately separate from authentication audit bodies.
// It lives in the primary database even when LOG_DB is a ClickHouse store.
type ConversationLog struct {
	Id                 int    `json:"-"`
	RequestId          string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId             int    `json:"user_id" gorm:"index"`
	CreatedAt          int64  `json:"created_at" gorm:"index"`
	Model              string `json:"model" gorm:"type:varchar(255)"`
	ResponseModel      string `json:"response_model" gorm:"type:varchar(255)"`
	TokenId            int    `json:"token_id"`
	TokenName          string `json:"token_name" gorm:"type:varchar(255)"`
	Protocol           string `json:"protocol" gorm:"type:varchar(32)"`
	Kind               string `json:"kind" gorm:"type:varchar(32)"`
	ResponseId         string `json:"response_id" gorm:"type:varchar(255);index"`
	PreviousResponseId string `json:"previous_response_id" gorm:"type:varchar(255);index"`
	Outcome            string `json:"outcome" gorm:"type:varchar(32)"`
	Truncated          bool   `json:"truncated"`
	Payload            []byte `json:"-" gorm:"size:16777216"`
	SessionKey         string `json:"session_key" gorm:"column:replay_key;type:varchar(96);index:idx_conv_log_replay_key"`
	SessionState       string `json:"session_state" gorm:"column:replay_state;type:varchar(16);index:idx_conv_log_replay_state"`
	NewUserMessages    int    `json:"new_user_messages" gorm:"column:replay_new_user_messages"`
	AssistantTextChars int    `json:"assistant_text_chars" gorm:"column:replay_assistant_text_chars"`
	ToolCalls          int    `json:"tool_calls" gorm:"column:replay_tool_calls"`
}

type ConversationReading struct {
	Input   []ConversationUserItem `json:"input"`
	Replies []ConversationUserItem `json:"replies"`
}

type ConversationOutputSpan struct {
	Item  int    `json:"item"`
	Part  int    `json:"part"`
	Kind  string `json:"kind"`
	Bytes int    `json:"bytes"`
}
type ConversationContent struct {
	OutputSpans     []ConversationOutputSpan `json:"output_spans,omitempty"`
	ClientReference string                   `json:"client_reference,omitempty"`
	Reading         *ConversationReading     `json:"reading,omitempty"`
	Request         string                   `json:"request"`
	Response        string                   `json:"response"`
	LatestInput     string                   `json:"latest_input"`
	Output          string                   `json:"output"`
}

func compressConversationPayload(encoded []byte) []byte {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(encoded); err != nil {
		return nil
	}
	if err := writer.Close(); err != nil {
		return nil
	}
	return compressed.Bytes()
}

func StoreConversation(entry *ConversationLog, content ConversationContent, audit AuditLog) error {
	conversationLinks(entry, content)
	encoded, err := common.Marshal(content)
	if err != nil {
		return err
	}
	entry.Payload = compressConversationPayload(encoded)
	if entry.Payload == nil {
		return gorm.ErrInvalidData
	}
	if err = DB.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}).Create(entry).Error; err != nil {
		return err
	}
	// No prompt, response, credentials or bearer token is stored in audit_logs.
	RecordAuditLog(nil, audit)
	return nil
}

// conversationLinks uses only explicit protocol identifiers, never text similarity.
func conversationLinks(entry *ConversationLog, content ConversationContent) {
	var request map[string]any
	_ = common.UnmarshalJsonStr(content.Request, &request)
	if nested, ok := request["response"].(map[string]any); ok {
		request = nested
	}
	if entry.Kind == "" {
		entry.Kind = "chat"
		_, input := request["input"]
		_, instructions := request["instructions"]
		if input || instructions || request["previous_response_id"] != nil || entry.Protocol == "websocket" {
			entry.Kind = "responses"
		}
	}
	if entry.Kind != "responses" {
		return
	}
	entry.PreviousResponseId, _ = request["previous_response_id"].(string)
	var response any
	_ = common.UnmarshalJsonStr(content.Response, &response)
	events, ok := response.([]any)
	if !ok {
		events = []any{response}
	}
	for _, value := range events {
		event, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if nested, ok := event["response"].(map[string]any); ok {
			event = nested
		} else if _, isEvent := event["type"]; isEvent {
			continue
		}
		if id, ok := event["id"].(string); ok && id != "" {
			entry.ResponseId = id
		}
	}
}

// BackfillConversationLinks enriches already recorded bodies after an upgrade.
func BackfillConversationLinks() error {
	for {
		var rows []ConversationLog
		if err := DB.Omit("payload").Where("kind = ? OR kind IS NULL", "").Limit(100).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			entry, content, err := GetConversation(row.RequestId, 0)
			if err != nil {
				return err
			}
			conversationLinks(entry, *content)
			if err = DB.Model(&ConversationLog{}).Where("id = ?", row.Id).Updates(map[string]any{"kind": entry.Kind, "response_id": entry.ResponseId, "previous_response_id": entry.PreviousResponseId}).Error; err != nil {
				return err
			}
		}
	}
}

type ConversationTurn struct {
	Record          ConversationLog      `json:"record"`
	ParentRequestId string               `json:"parent_request_id"`
	Content         *ConversationContent `json:"content"`
}

type ConversationThread struct {
	Turns             []ConversationTurn `json:"turns"`
	SelectedRequestId string             `json:"selected_request_id"`
	MissingPrevious   bool               `json:"missing_previous"`
	Truncated         bool               `json:"truncated"`
}

// GetConversationThread follows explicit links inside one user's retained records.
// A root request with full input history remains a useful standalone snapshot.
func GetConversationThread(requestId string, cutoff int64) (*ConversationThread, error) {
	selected, _, err := GetConversation(requestId, cutoff)
	if err != nil {
		return nil, err
	}
	result := &ConversationThread{SelectedRequestId: requestId}
	rows := map[string]ConversationLog{requestId: *selected}
	current := *selected
	for current.Kind == "responses" && current.PreviousResponseId != "" && len(rows) < 50 {
		var parents []ConversationLog
		if err = DB.Omit("payload").Where("user_id = ? AND token_id = ? AND kind = ? AND response_id = ? AND created_at >= ?", selected.UserId, selected.TokenId, "responses", current.PreviousResponseId, cutoff).Limit(2).Find(&parents).Error; err != nil {
			return nil, err
		}
		if len(parents) != 1 {
			result.MissingPrevious = true
			break
		}
		if _, seen := rows[parents[0].RequestId]; seen {
			result.MissingPrevious = true
			break
		}
		current = parents[0]
		rows[current.RequestId] = current
	}
	if len(rows) == 50 {
		result.Truncated = true
	}
	frontier := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.ResponseId != "" {
			frontier = append(frontier, row.ResponseId)
		}
	}
	for len(frontier) > 0 && len(rows) < 50 && selected.Kind == "responses" {
		var children []ConversationLog
		if err = DB.Omit("payload").Where("user_id = ? AND token_id = ? AND kind = ? AND previous_response_id IN ? AND created_at >= ?", selected.UserId, selected.TokenId, "responses", frontier, cutoff).Order("created_at, id").Limit(51).Find(&children).Error; err != nil {
			return nil, err
		}
		frontier = nil
		for _, row := range children {
			if _, seen := rows[row.RequestId]; seen {
				continue
			}
			if len(rows) == 50 {
				result.Truncated = true
				break
			}
			rows[row.RequestId] = row
			if row.ResponseId != "" {
				frontier = append(frontier, row.ResponseId)
			}
		}
	}
	ordered := make([]ConversationLog, 0, len(rows))
	if len(rows) == 50 {
		result.Truncated = true
	}
	for _, row := range rows {
		ordered = append(ordered, row)
	}
	slices.SortFunc(ordered, func(a, b ConversationLog) int {
		if a.CreatedAt != b.CreatedAt {
			return cmp.Compare(a.CreatedAt, b.CreatedAt)
		}
		return cmp.Compare(a.Id, b.Id)
	})
	parents := map[string]string{}
	for _, row := range ordered {
		if row.ResponseId != "" {
			if _, duplicate := parents[row.ResponseId]; duplicate {
				parents[row.ResponseId] = ""
			} else {
				parents[row.ResponseId] = row.RequestId
			}
		}
	}
	remaining := 8 << 20
	// Prefer the selected turn if a long thread exceeds the body transfer budget.
	contents := map[string]*ConversationContent{}
	loadOrder := append([]ConversationLog{*selected}, ordered...)
	for _, row := range loadOrder {
		if _, seen := contents[row.RequestId]; seen {
			continue
		}
		_, content, loadErr := GetConversation(row.RequestId, cutoff)
		if loadErr != nil {
			return nil, loadErr
		}
		size := len(content.Request) + len(content.Response) + len(content.LatestInput) + len(content.Output)
		if size > remaining {
			result.Truncated = true
			contents[row.RequestId] = nil
			continue
		}
		remaining -= size
		contents[row.RequestId] = content
	}
	for _, row := range ordered {
		row.Payload = nil
		result.Turns = append(result.Turns, ConversationTurn{Record: row, ParentRequestId: parents[row.PreviousResponseId], Content: contents[row.RequestId]})
	}
	return result, nil
}

func GetConversation(requestId string, cutoff int64) (*ConversationLog, *ConversationContent, error) {
	var entry ConversationLog
	if err := DB.Where("request_id = ? AND created_at >= ?", requestId, cutoff).First(&entry).Error; err != nil {
		return nil, nil, err
	}
	if len(entry.Payload) == 0 {
		return &entry, &ConversationContent{}, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(entry.Payload))
	if err != nil {
		return &entry, nil, err
	}
	defer reader.Close()
	encoded, err := io.ReadAll(io.LimitReader(reader, 40<<20))
	if err != nil {
		return &entry, nil, err
	}
	var content ConversationContent
	if err = common.Unmarshal(encoded, &content); err != nil {
		return &entry, nil, err
	}
	return &entry, &content, nil
}

func CleanupConversations(cutoff int64) error {
	if err := DB.Where("created_at < ?", cutoff).Delete(&ConversationLog{}).Error; err != nil {
		return err
	}
	if DB.Migrator().HasTable(&QATurn{}) {
		expired := DB.Model(&QATurn{}).Select("id").Where("created_at < ?", cutoff)
		if err := DB.Model(&QAReceipt{}).Where("turn_id IN (?)", expired).Updates(map[string]any{"turn_id": 0, "state": "expired"}).Error; err != nil {
			return err
		}
		if err := DB.Where("turn_id IN (?)", expired).Delete(&QAEdge{}).Error; err != nil {
			return err
		}
	}
	for _, table := range []any{&QAEdge{}, &QAReceipt{}, &QATurn{}} {
		if DB.Migrator().HasTable(table) {
			if err := DB.Where("created_at < ?", cutoff).Delete(table).Error; err != nil {
				return err
			}
		}
	}
	if err := CleanupConversationSessions(cutoff); err != nil {
		return err
	}
	// Only this new category expires; authentication audit retention is unchanged.
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return LOG_DB.Exec("ALTER TABLE audit_logs DELETE WHERE category = ? AND created_at < ? SETTINGS mutations_sync = 1", AuditCategoryConversation, cutoff).Error
	}
	return LOG_DB.Where("category = ? AND created_at < ?", AuditCategoryConversation, cutoff).Delete(&AuditLog{}).Error
}

func ConversationCutoff() int64 {
	days := common.GetEnvOrDefault("CONVERSATION_RETENTION_DAYS", 0)
	if days == 0 {
		days = common.GetEnvOrDefault("CONVERSATION_LOG_RETENTION_DAYS", 30)
	}
	days = min(max(days, 1), 90)
	return time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
}
