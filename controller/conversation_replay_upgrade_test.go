package controller

import (
	"bytes"
	"compress/gzip"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
)

// The external DSNs must point only to disposable, empty test databases.
func TestConversationReplayUpgradeMatrix(t *testing.T) {
	type oldLog struct {
		Id           int    `gorm:"primaryKey"`
		RequestId    string `gorm:"type:varchar(64)"`
		UserId       int
		TokenId      int
		CreatedAt    int64
		Payload      []byte `gorm:"size:16777216"`
		SessionKey   string
		SessionState string
		ToolCalls    int
		Outcome      string
	}
	type oldMessage struct {
		Id          int64  `gorm:"primaryKey"`
		RequestId   string `gorm:"type:varchar(64)"`
		Role        string
		Kind        string
		Content     string
		ContentHash string
		Truncated   bool
	}
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var driver gorm.Dialector
			switch engine {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("no disposable MySQL")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("no disposable PostgreSQL")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			conn, err := db.DB()
			require.NoError(t, err)
			conn.SetMaxOpenConns(1)
			prev := model.DB
			model.DB = db
			t.Cleanup(func() { model.DB = prev; _ = conn.Close() })
			require.False(t, db.Migrator().HasTable("conversation_logs"), "requires an empty dedicated schema")
			require.NoError(t, db.Table("conversation_logs").AutoMigrate(&oldLog{}))
			require.NoError(t, db.Table("conversation_messages").AutoMigrate(&oldMessage{}))
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&model.ConversationMessage{}, &model.ConversationSession{}, "conversation_messages", "conversation_logs"))
			})
			archive := func(c model.ConversationContent) []byte {
				b, e := common.Marshal(c)
				require.NoError(t, e)
				var buf bytes.Buffer
				z := gzip.NewWriter(&buf)
				_, e = z.Write(b)
				require.NoError(t, e)
				require.NoError(t, z.Close())
				return buf.Bytes()
			}
			raw := archive(model.ConversationContent{Request: `{"input":[{"role":"user","content":"A real question"},{"role":"user","content":"<environment_context>demo</environment_context>"}]}`, Response: `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"A visible answer"}]}]}`})
			extra := archive(model.ConversationContent{Request: `{"capture":"request exceeds recording limit"}`, Response: `[{"type":"response.reasoning_summary_text.done","text":"summary"}]`, Output: "summary"})
			require.NoError(t, db.Table("conversation_logs").Create(&[]oldLog{{RequestId: "first", UserId: 1, TokenId: 2, CreatedAt: 100, Payload: raw, SessionKey: "v1", SessionState: "linked", Outcome: "completed"}, {RequestId: "lost", UserId: 1, TokenId: 2, CreatedAt: 101, Payload: extra, SessionKey: "v1", SessionState: "linked", ToolCalls: 2, Outcome: "interrupted"}}).Error)
			require.NoError(t, db.Table("conversation_messages").Create(&oldMessage{RequestId: "lost", Role: "user", Kind: "text", Content: "Old surviving text without a boundary", Truncated: true}).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&model.ConversationLog{}, &model.ConversationSession{}, &model.ConversationMessage{}))
				service.BackfillConversationSessions()
			}
			var old []oldLog
			require.NoError(t, db.Table("conversation_logs").Order("id").Find(&old).Error)
			require.Len(t, old, 2)
			require.Equal(t, raw, old[0].Payload)
			require.Equal(t, extra, old[1].Payload)
			require.Equal(t, "v1", old[0].SessionKey)
			var messages []model.ConversationMessage
			require.NoError(t, db.Order("id").Find(&messages).Error)
			require.Len(t, messages, 4)
			require.Equal(t, "context", messages[1].Kind)
			require.Equal(t, "A visible answer", messages[2].Content)
			require.Equal(t, "unknown", messages[3].Kind)
			require.True(t, messages[3].Truncated)
			entry, c, err := model.GetConversation("first", 0)
			require.NoError(t, err)
			require.NoError(t, service.ApplyConversationReading(entry, c))
			require.Equal(t, "A real question", c.LatestInput)
			require.Equal(t, "A visible answer", c.Output)
			var lost model.ConversationLog
			require.NoError(t, db.Where("request_id = ?", "lost").First(&lost).Error)
			require.Equal(t, 2, lost.ToolCalls)
			require.Zero(t, lost.NewUserMessages)
		})
	}
}
