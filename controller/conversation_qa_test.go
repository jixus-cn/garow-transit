package controller

import (
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestConversationQAStorageMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var d gorm.Dialector
			switch engine {
			case "sqlite":
				d = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("dedicated MySQL not configured")
				}
				d = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("dedicated PostgreSQL not configured")
				}
				d = postgres.Open(dsn)
			}
			db, e := gorm.Open(d, &gorm.Config{})
			require.NoError(t, e)
			sql, e := db.DB()
			require.NoError(t, e)
			sql.SetMaxOpenConns(1)
			prev, log := model.DB, model.LOG_DB
			model.DB, model.LOG_DB = db, db
			t.Cleanup(func() { model.DB, model.LOG_DB = prev, log; _ = sql.Close() })
			v := "SELECT version()"
			if engine == "sqlite" {
				v = "SELECT sqlite_version()"
			}
			var ver string
			require.NoError(t, db.Raw(v).Scan(&ver).Error)
			t.Log("database version:", ver)
			now := time.Now().Unix()
			week := int64(7 * 24 * 3600)
			for _, upgrade := range []bool{false, true} {
				t.Run(fmt.Sprintf("upgrade=%v", upgrade), func(t *testing.T) {
					tables := []any{&model.QATurn{}, &model.QAReceipt{}, &model.QAEdge{}}
					for _, tab := range tables {
						require.False(t, db.Migrator().HasTable(tab), "requires a dedicated empty database")
					}
					t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(tables...)) })
					if upgrade {
						// .11 uses exactly these QA schemas. Preserve a verified chat and an unassigned turn.
						require.NoError(t, db.AutoMigrate(tables...))
						for _, tc := range []struct{ id, chat string }{{"known", "stable"}, {"old", ""}} {
							require.NoError(t, model.StoreQA(model.QARequestMeta{RequestId: tc.id, UserId: 1, CreatedAt: now}, model.QAFacts{Chat: tc.chat, Prompt: "historical", Final: "retained"}, 0))
						}
					}
					for range 2 {
						require.NoError(t, db.AutoMigrate(tables...))
					}
					if upgrade {
						x, err := model.QAByRequest("old", 0)
						require.NoError(t, err)
						require.Empty(t, x.ChatKey)
						require.Equal(t, "retained", x.Final)
						x, err = model.QAByRequest("known", 0)
						require.NoError(t, err)
						require.NotEmpty(t, x.ChatKey)
						require.Equal(t, "retained", x.Final)
					}
					write := func(id string, at int64, key int, f model.QAFacts) {
						t.Helper()
						if f.Client == "" {
							f.Client = "fixture"
						}
						r := model.QARequestMeta{RequestId: id, CreatedAt: at, UserId: 2, TokenId: key}
						require.NoError(t, model.StoreQA(r, f, 0))
					}
					read := func(id string) *model.QATurn {
						t.Helper()
						turn, e := model.QAByRequest(id, 0)
						require.NoError(t, e)
						return turn
					}
					write("q1", now-3*week, 1, model.QAFacts{Chat: "A", Submission: "s1", Prompt: "Continue", Issued: []string{"t1", "t2"}, Response: "parent"})
					write("q2", now-week, 1, model.QAFacts{Chat: "A", Submission: "s2", Prompt: "Continue", Final: "second"})
					write("q3", now-week, 1, model.QAFacts{Chat: "B", Prompt: "Continue", Final: "other chat"})
					write("cross-key", now, 2, model.QAFacts{Returned: []string{"t1"}, Final: "unrelated"})
					require.Nil(t, read("cross-key"))
					write("cross-client", now, 1, model.QAFacts{Client: "other", Returned: []string{"t1"}, Final: "unrelated"})
					require.Nil(t, read("cross-client"))
					write("wrong-chat", now, 1, model.QAFacts{Chat: "B", Parent: "parent", Returned: []string{"t1"}, Final: "unrelated"})
					require.Nil(t, read("wrong-chat"))
					write("both", now, 1, model.QAFacts{Parent: "parent", Returned: []string{"t1", "t2"}, Final: "first"})
					write("both", now, 1, model.QAFacts{Final: "replay"})
					require.Equal(t, read("q1").Id, read("both").Id)
					require.Equal(t, "first", read("q1").Final)
					write("same-submission", now, 1, model.QAFacts{Chat: "A", Submission: "s2", Prompt: "Continue", Final: "second"})
					require.Equal(t, read("q2").Id, read("same-submission").Id)
					require.NotEqual(t, read("q1").Id, read("q2").Id)
					write("unassigned", now, 1, model.QAFacts{Prompt: "unassigned 100%", Final: "ok"})
					write("interrupted", now, 1, model.QAFacts{Prompt: "only question"})
					require.Empty(t, read("interrupted").Final)
					filter := model.QAFilter{User: 2}
					chats, n, e := model.QAChats(filter, 1, 10)
					require.NoError(t, e)
					require.Len(t, chats, 2)
					require.EqualValues(t, 2, n)
					oldChat := read("q1").ChatKey
					turns, n, e := model.QATurns(filter, oldChat, 1, 10)
					require.NoError(t, e)
					require.EqualValues(t, 2, n)
					require.Equal(t, read("q1").Id, turns[0].Id)
					_, n, e = model.QATurns(model.QAFilter{Query: "%"}, "*", 1, 10)
					require.NoError(t, e)
					require.EqualValues(t, 1, n)
					// Colliding call IDs never connect two independently established turns.
					write("ca", now, 1, model.QAFacts{Prompt: "a", Issued: []string{"collision"}})
					write("cb", now, 1, model.QAFacts{Prompt: "b", Issued: []string{"collision"}})
					write("cx", now, 1, model.QAFacts{Returned: []string{"collision"}, Final: "ambiguous"})
					require.Nil(t, read("cx"))
					write("p0", now, 1, model.QAFacts{Prompt: "standalone", Response: "p0"})
					require.Empty(t, read("p0").ChatKey)
					write("p1", now, 1, model.QAFacts{Prompt: "next", Parent: "p0", Final: "new"})
					require.NotEmpty(t, read("p0").ChatKey)
					require.Equal(t, read("p0").ChatKey, read("p1").ChatKey)
					require.NoError(t, model.CleanupConversations(now-week+1))
					write("after-expiry", now+1, 1, model.QAFacts{Chat: "A", Prompt: "reopened"})
					require.Equal(t, oldChat, read("after-expiry").ChatKey)
					_, e = model.QAByRequest("q1", now-week+1)
					require.ErrorIs(t, e, gorm.ErrRecordNotFound)
					for _, obsolete := range []string{"conversation_logs", "conversation_sessions_v2", "conversation_messages_v2"} {
						require.False(t, db.Migrator().HasTable(obsolete))
					}
				})
			}
		})
	}
}
func TestConversationQACaptureWriter(t *testing.T) {
	t.Setenv("CONVERSATION_LOG_ENABLED", "true")
	t.Setenv("CONVERSATION_LOG_MODE", "full") // Retired settings must not enable legacy capture.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sql, err := db.DB()
	require.NoError(t, err)
	sql.SetMaxOpenConns(1)
	before, logs := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() { model.DB, model.LOG_DB = before, logs; _ = sql.Close() })
	require.NoError(t, db.AutoMigrate(&model.QATurn{}, &model.QAReceipt{}, &model.QAEdge{}, &model.AuditLog{}, &model.User{}))
	service.StartConversationRecorder()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"system","content":"excluded"},{"role":"user","content":"captured"}]}`))
	c.Set("id", 1)
	c.Set(common.RequestIdKey, "qa-e2e")
	c.Set("username", "fixture")
	cap := service.BeginConversation(c, "http")
	require.NotNil(t, cap)
	cap.Observe([]byte(`{"choices":[{"message":{"content":"delivered"},"finish_reason":"stop"}]}`))
	cap.Finish(200, false)
	service.FlushConversations(4 * time.Second)
	turn, err := model.QAByRequest("qa-e2e", 0)
	require.NoError(t, err)
	require.NotNil(t, turn)
	require.Equal(t, "captured", turn.Prompt)
	require.Equal(t, "delivered", turn.Final)
	var n int64
	require.NoError(t, db.Model(&model.AuditLog{}).Count(&n).Error)
	require.Zero(t, n)
	require.False(t, db.Migrator().HasTable("conversation_logs"))
}

func TestConversationQARootSessionRequired(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	prev := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = prev; sql, _ := db.DB(); _ = sql.Close() })
	require.NoError(t, db.AutoMigrate(&model.QATurn{}, &model.QAReceipt{}, &model.QAEdge{}))
	require.NoError(t, model.StoreQA(model.QARequestMeta{RequestId: "request", CreatedAt: time.Now().Unix(), UserId: 1}, model.QAFacts{Prompt: "question", Final: "answer"}, 0))
	for _, session := range []bool{false, true} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/conversations/unassigned", nil)
		c.Set("id", 1)
		c.Set("role", 100)
		if session {
			c.Set("session_id", "synthetic")
			c.Set("auth_version", int64(1))
			c.Set("session_version", int64(1))
		}
		GetUnassignedQA(c)
		if !session {
			require.Equal(t, 403, w.Code)
			require.NotContains(t, w.Body.String(), "question")
			continue
		}
		require.Equal(t, 200, w.Code)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var resp struct {
			Data struct{ Items []map[string]any }
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Data.Items, 1)
		require.Len(t, resp.Data.Items[0], 6)
		require.Equal(t, "answer", resp.Data.Items[0]["final"])
	}
}

func TestConversationEventsRetiredFromGeneralAudit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previous := model.LOG_DB
	model.LOG_DB = db
	t.Cleanup(func() { model.LOG_DB = previous; conn, _ := db.DB(); _ = conn.Close() })
	require.NoError(t, db.AutoMigrate(&model.AuditLog{}))
	for _, category := range []string{"conversation", "login", "operation"} {
		require.NoError(t, db.Create(&model.AuditLog{EventId: category, Category: category, ActorRole: common.RoleRootUser}).Error)
	}
	rows, total, err := model.GetAuditLogs(model.AuditLogFilter{}, 0, 10, common.RoleRootUser)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.ElementsMatch(t, []string{"login", "operation"}, []string{rows[0].Category, rows[1].Category})
	require.False(t, model.ValidAuditCategory("conversation"))
}
