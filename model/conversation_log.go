package model

import (
	"bytes"
	"compress/gzip"
	"io"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// ConversationLog is deliberately separate from authentication audit bodies.
// It lives in the primary database even when LOG_DB is a ClickHouse store.
type ConversationLog struct {
	Id            int    `json:"-"`
	RequestId     string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId        int    `json:"user_id" gorm:"index"`
	CreatedAt     int64  `json:"created_at" gorm:"index"`
	Model         string `json:"model" gorm:"type:varchar(255)"`
	ResponseModel string `json:"response_model" gorm:"type:varchar(255)"`
	TokenId       int    `json:"token_id"`
	TokenName     string `json:"token_name" gorm:"type:varchar(255)"`
	Protocol      string `json:"protocol" gorm:"type:varchar(32)"`
	Outcome       string `json:"outcome" gorm:"type:varchar(32)"`
	Truncated     bool   `json:"truncated"`
	Payload       []byte `json:"-" gorm:"size:16777216"`
}

type ConversationContent struct {
	Request     string `json:"request"`
	Response    string `json:"response"`
	LatestInput string `json:"latest_input"`
	Output      string `json:"output"`
}

func StoreConversation(entry *ConversationLog, content ConversationContent, audit AuditLog) error {
	encoded, err := common.Marshal(content)
	if err != nil {
		return err
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err = writer.Write(encoded); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	entry.Payload = compressed.Bytes()
	if err = DB.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}).Create(entry).Error; err != nil {
		return err
	}
	// No prompt, response, credentials or bearer token is stored in audit_logs.
	RecordAuditLog(nil, audit)
	return nil
}

func GetConversation(requestId string, cutoff int64) (*ConversationLog, *ConversationContent, error) {
	var entry ConversationLog
	if err := DB.Where("request_id = ? AND created_at >= ?", requestId, cutoff).First(&entry).Error; err != nil {
		return nil, nil, err
	}
	reader, err := gzip.NewReader(bytes.NewReader(entry.Payload))
	if err != nil {
		return nil, nil, err
	}
	defer reader.Close()
	encoded, err := io.ReadAll(io.LimitReader(reader, 40<<20))
	if err != nil {
		return nil, nil, err
	}
	var content ConversationContent
	if err = common.Unmarshal(encoded, &content); err != nil {
		return nil, nil, err
	}
	return &entry, &content, nil
}

func CleanupConversations(cutoff int64) error {
	if err := DB.Where("created_at < ?", cutoff).Delete(&ConversationLog{}).Error; err != nil {
		return err
	}
	// Only this new category expires; authentication audit retention is unchanged.
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return LOG_DB.Exec("ALTER TABLE audit_logs DELETE WHERE category = ? AND created_at < ? SETTINGS mutations_sync = 1", AuditCategoryConversation, cutoff).Error
	}
	return LOG_DB.Where("category = ? AND created_at < ?", AuditCategoryConversation, cutoff).Delete(&AuditLog{}).Error
}

func ConversationCutoff() int64 {
	days := min(max(common.GetEnvOrDefault("CONVERSATION_LOG_RETENTION_DAYS", 7), 1), 90)
	return time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
}
