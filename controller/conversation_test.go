package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestConversationStorageMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				if os.Getenv("TEST_MYSQL_DSN") == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
			case "postgres":
				if os.Getenv("TEST_POSTGRES_DSN") == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.New(postgres.Config{DSN: os.Getenv("TEST_POSTGRES_DSN"), PreferSimpleProtocol: true})
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			logDriver := sqlite.Open(":memory:")
			var separateDriver gorm.Dialector = logDriver
			if dialect == "mysql" {
				require.NotEmpty(t, os.Getenv("TEST_LOG_MYSQL_DSN"))
				separateDriver = mysql.Open(os.Getenv("TEST_LOG_MYSQL_DSN"))
			} else if dialect == "postgres" {
				require.NotEmpty(t, os.Getenv("TEST_LOG_POSTGRES_DSN"))
				separateDriver = postgres.New(postgres.Config{DSN: os.Getenv("TEST_LOG_POSTGRES_DSN"), PreferSimpleProtocol: true})
			}
			logDB, err := gorm.Open(separateDriver, &gorm.Config{})
			require.NoError(t, err)
			logSQL, err := logDB.DB()
			require.NoError(t, err)
			logSQL.SetMaxOpenConns(1)
			previousDB, previousLogDB, previousType := model.DB, model.LOG_DB, common.LogDatabaseType()
			model.DB, model.LOG_DB = db, logDB
			common.SetLogDatabaseType(common.DatabaseType(dialect))
			t.Cleanup(func() {
				model.DB, model.LOG_DB = previousDB, previousLogDB
				common.SetLogDatabaseType(previousType)
				assert.NoError(t, sqlDB.Close())
				assert.NoError(t, logSQL.Close())
			})
			versionSQL := "SELECT version()"
			if dialect == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
			t.Logf("database version: %s", version)
			for _, upgrade := range []bool{false, true} {
				t.Run(fmt.Sprintf("upgrade=%t", upgrade), func(t *testing.T) {
					require.NoError(t, logDB.AutoMigrate(&model.AuditLog{}))
					if upgrade {
						require.NoError(t, db.AutoMigrate(&releasedConversationLog{}))
						require.NoError(t, db.Create(&releasedConversationLog{RequestId: "legacy-record", UserId: 99, CreatedAt: 10, Payload: []byte("legacy-compressed-content")}).Error)
						require.NoError(t, logDB.Create(&model.AuditLog{EventId: "existing-audit", CreatedAt: 10, Category: model.AuditCategoryLogin, ActorRole: 1, Content: "existing"}).Error)
					}
					for range 2 {
						require.NoError(t, db.AutoMigrate(&model.ConversationLog{}))
					}
					if upgrade {
						var legacy model.ConversationLog
						require.NoError(t, db.Where("request_id = ?", "legacy-record").First(&legacy).Error)
						assert.Equal(t, []byte("legacy-compressed-content"), legacy.Payload)
					}
					row := model.ConversationLog{RequestId: "request-1", CreatedAt: time.Now().Unix(), UserId: 1, Model: "test-model", Outcome: "completed"}
					body := model.ConversationContent{Request: strings.Repeat("测试完整请求", 20000), Response: "reply", LatestInput: "hello", Output: "reply"}
					audit := model.AuditLog{EventId: "request-1", RequestId: "request-1", UserId: 1, Username: "fixture-user", ActorRole: 1, CreatedAt: row.CreatedAt, Category: model.AuditCategoryConversation, Content: "Conversation · test-model", Success: true}
					require.NoError(t, model.StoreConversation(&row, body, audit))
					for range 2 {
						require.NoError(t, db.AutoMigrate(&model.ConversationLog{}))
					}
					stored, content, err := model.GetConversation("request-1", 0)
					require.NoError(t, err)
					assert.Equal(t, row.RequestId, stored.RequestId)
					assert.Equal(t, body, *content)
					duplicate := model.ConversationLog{RequestId: "request-1"}
					assert.Error(t, db.Create(&duplicate).Error)
					_, total, err := model.GetAuditLogs(model.AuditLogFilter{Category: model.AuditCategoryConversation}, 0, 10, common.RoleRootUser)
					require.NoError(t, err)
					assert.EqualValues(t, 1, total)
					for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
						_, total, err = model.GetAuditLogs(model.AuditLogFilter{}, 0, 10, role)
						require.NoError(t, err)
						if upgrade {
							assert.EqualValues(t, 1, total)
						} else {
							assert.Zero(t, total)
						}
					}
					_, total, err = model.GetAuditLogs(model.AuditLogFilter{SelfView: true}, 0, 10, common.RoleRootUser)
					require.NoError(t, err)
					if upgrade {
						assert.EqualValues(t, 1, total)
					} else {
						assert.Zero(t, total)
					}
					require.NoError(t, model.CleanupConversations(time.Now().Add(time.Hour).Unix()))
					_, _, err = model.GetConversation("request-1", 0)
					assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
					var count int64
					require.NoError(t, logDB.Model(&model.AuditLog{}).Where("event_id = ?", "existing-audit").Count(&count).Error)
					if upgrade {
						assert.EqualValues(t, 1, count)
					}
					testConversationThreadLinks(t, db)
					require.NoError(t, db.Migrator().DropTable(&model.ConversationLog{}))
					require.NoError(t, logDB.Migrator().DropTable(&model.AuditLog{}))
				})
			}
		})
	}
}

type releasedConversationLog struct {
	Id            int
	RequestId     string `gorm:"type:varchar(64);uniqueIndex"`
	UserId        int    `gorm:"index"`
	CreatedAt     int64  `gorm:"index"`
	Model         string `gorm:"type:varchar(255)"`
	ResponseModel string `gorm:"type:varchar(255)"`
	TokenId       int
	TokenName     string `gorm:"type:varchar(255)"`
	Protocol      string `gorm:"type:varchar(32)"`
	Outcome       string `gorm:"type:varchar(32)"`
	Truncated     bool
	Payload       []byte `gorm:"size:16777216"`
}

func (releasedConversationLog) TableName() string { return "conversation_logs" }

func testConversationThreadLinks(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, fixture := range []struct {
		id, response, previous string
		user                   int
	}{
		{"root", "resp-root", "", 1}, {"branch-a", "resp-a", "resp-root", 1},
		{"branch-b", "resp-b", "resp-root", 1}, {"tool-turn", "resp-tool", "resp-a", 1},
		{"another-user", "resp-other", "resp-root", 2},
	} {
		row := model.ConversationLog{RequestId: fixture.id, CreatedAt: time.Now().Unix(), UserId: fixture.user, Kind: "responses"}
		content := model.ConversationContent{Request: fmt.Sprintf(`{"input":"question","previous_response_id":%q}`, fixture.previous), Response: fmt.Sprintf(`{"id":%q,"output":[{"role":"assistant","content":[{"type":"output_text","text":"reply"}]}]}`, fixture.response)}
		require.NoError(t, model.StoreConversation(&row, content, model.AuditLog{EventId: fixture.id, UserId: fixture.user, Username: "fixture", ActorRole: 1, Category: model.AuditCategoryConversation}))
	}
	require.NoError(t, db.Model(&model.ConversationLog{}).Where("request_id = ?", "branch-a").Updates(map[string]any{"kind": "", "response_id": "", "previous_response_id": ""}).Error)
	require.NoError(t, model.BackfillConversationLinks())
	thread, err := model.GetConversationThread("branch-a", 0)
	require.NoError(t, err)
	require.Len(t, thread.Turns, 4)
	parents := map[string]string{}
	for _, turn := range thread.Turns {
		parents[turn.Record.RequestId] = turn.ParentRequestId
		assert.Equal(t, 1, turn.Record.UserId)
		require.NotNil(t, turn.Content)
	}
	assert.Equal(t, "root", parents["branch-a"])
	assert.Equal(t, "root", parents["branch-b"])
	assert.Equal(t, "branch-a", parents["tool-turn"])
	assert.False(t, thread.MissingPrevious)
	require.NoError(t, db.Model(&model.ConversationLog{}).Where("request_id = ?", "root").Update("created_at", 1).Error)
	thread, err = model.GetConversationThread("branch-a", time.Now().Unix()-60)
	require.NoError(t, err)
	assert.True(t, thread.MissingPrevious)
	assert.Len(t, thread.Turns, 2)
	for index := range 52 {
		previous := ""
		if index > 0 {
			previous = fmt.Sprintf("limit-response-%d", index-1)
		}
		row := model.ConversationLog{RequestId: fmt.Sprintf("limit-request-%d", index), UserId: 3, Kind: "responses", CreatedAt: time.Now().Unix()}
		content := model.ConversationContent{Request: fmt.Sprintf(`{"input":"fixture","previous_response_id":%q}`, previous), Response: fmt.Sprintf(`{"id":"limit-response-%d","output":[]}`, index)}
		require.NoError(t, model.StoreConversation(&row, content, model.AuditLog{EventId: row.RequestId, Username: "fixture", ActorRole: 1, Category: model.AuditCategoryConversation}))
	}
	thread, err = model.GetConversationThread("limit-request-51", 0)
	require.NoError(t, err)
	assert.True(t, thread.Truncated)
	assert.Len(t, thread.Turns, 50)
}

func TestConversationCaptureAndAccess(t *testing.T) {
	t.Setenv("CONVERSATION_LOG_ENABLED", "true")
	t.Setenv("CONVERSATION_LOG_MAX_BYTES", "65536")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() { model.DB, model.LOG_DB = previousDB, previousLogDB; assert.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.ConversationLog{}, &model.AuditLog{}))
	require.NoError(t, db.Exec("CREATE TABLE users (id integer primary key, username text, role integer, deleted_at datetime)").Error)
	require.NoError(t, db.Exec("INSERT INTO users VALUES (1, 'fixture-user', 1, NULL)").Error)
	service.StartConversationRecorder()
	t.Run("HTTP middleware preserves SSE bytes and flush", func(t *testing.T) {
		engine := gin.New()
		engine.Use(middleware.BodyStorageCleanup(), func(c *gin.Context) {
			c.Set("id", 1)
			c.Set(common.RequestIdKey, "http-wrapper")
		}, middleware.ConversationRecording())
		payload := "data: {\"choices\":[{\"delta\":{\"content\":\"stream reply\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
		engine.POST("/v1/chat/completions", func(c *gin.Context) {
			body, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			assert.Contains(t, string(body), "question")
			c.Header("Content-Type", "text/event-stream")
			_, err = c.Writer.WriteString(payload[:12])
			require.NoError(t, err)
			c.Writer.Flush()
			_, err = c.Writer.Write([]byte(payload[12:]))
			require.NoError(t, err)
		})
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fixture","messages":[{"role":"user","content":"question"}]}`)))
		assert.True(t, response.Flushed)
		assert.Equal(t, payload, response.Body.String())
		require.Eventually(t, func() bool {
			var count int64
			return db.Model(&model.AuditLog{}).Where("request_id = ?", "http-wrapper").Count(&count).Error == nil && count == 1
		}, 5*time.Second, 10*time.Millisecond)
		_, content, err := model.GetConversation("http-wrapper", 0)
		require.NoError(t, err)
		assert.Equal(t, "stream reply", content.Output)
	})
	for _, tc := range []struct {
		name, protocol, response, output, outcome string
		cancel, truncated                         bool
	}{
		{"json", "http", `{"model":"actual-model","choices":[{"message":{"content":"hello"},"finish_reason":"stop"}]}`, "hello", "completed", false, false},
		{"sse", "http", "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"lo\",\"tool_calls\":[{\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", "hello", "completed", false, false},
		{"responses", "http", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"actual-model\",\"output\":[]}}\n\n", "hello", "completed", false, false},
		{"websocket-1", "websocket", "{\"type\":\"response.output_text.delta\",\"delta\":\"first\"}\n{\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n", "first", "completed", false, false},
		{"websocket-2", "websocket", "{\"type\":\"response.output_text.delta\",\"delta\":\"second\"}\n", "second", "interrupted", true, false},
		{"truncated", "http", strings.Repeat("x", 70000), "", "incomplete", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestBody := `{"model":"requested-model","messages":[{"role":"user","content":"question"}],"tools":[{"type":"function","function":{"name":"read"}}],"api_key":"fixture-secret","metadata":{"secret":"fixture-secret"}}`
			engine := gin.New()
			engine.POST("/v1/chat/completions", middleware.BodyStorageCleanup(), func(c *gin.Context) {
				c.Set("id", 1)
				c.Set(common.RequestIdKey, tc.name)
				c.Set("token_id", 42)
				c.Set("token_name", "fixture-key")
				c.Next()
			}, func(c *gin.Context) {
				r := service.BeginConversation(c, tc.protocol)
				require.NotNil(t, r)
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				assert.Equal(t, requestBody, string(body), "recording must preserve downstream body")
				for i := 0; i < len(tc.response); i += 3 {
					r.Observe([]byte(tc.response[i:min(i+3, len(tc.response))]))
				}
				r.Finish(200, tc.cancel)
				r.Finish(200, false)
				c.Status(200)
			})
			request := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(requestBody))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer fixture-secret")
			engine.ServeHTTP(httptest.NewRecorder(), request)
			require.Eventually(t, func() bool {
				var count int64
				return db.Model(&model.AuditLog{}).Where("request_id = ?", tc.name).Count(&count).Error == nil && count == 1
			}, 5*time.Second, 10*time.Millisecond)
			entry, content, err := model.GetConversation(tc.name, 0)
			require.NoError(t, err)
			assert.Equal(t, tc.output, content.Output)
			assert.Equal(t, tc.outcome, entry.Outcome)
			assert.Equal(t, tc.truncated, entry.Truncated)
			assert.NotContains(t, content.Request, "fixture-secret")
			assert.Equal(t, "question", content.LatestInput)
			if tc.name == "sse" {
				assert.Contains(t, content.Response, "tool_calls")
			}
		})
	}
	for _, tc := range []struct {
		name    string
		role    int
		session bool
		id      string
		status  int
	}{
		{"root", 100, true, "json", 200}, {"user", 1, true, "json", 403}, {"admin", 10, true, "json", 403}, {"root-token", 100, false, "json", 403}, {"expired", 100, true, "missing", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, thread := range []bool{false, true} {
				engine := gin.New()
				engine.GET("/conversation/:request_id", func(c *gin.Context) {
					c.Set("id", 1)
					c.Set("role", tc.role)
					if tc.session {
						c.Set("session_id", "fixture-session")
						c.Set("auth_version", int64(1))
						c.Set("session_version", int64(1))
					}
					if thread {
						GetConversationThread(c)
					} else {
						GetConversation(c)
					}
				})
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, httptest.NewRequest("GET", "/conversation/"+tc.id, nil))
				assert.Equal(t, tc.status, response.Code)
				if tc.status == http.StatusOK {
					assert.Contains(t, response.Body.String(), "question")
					assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
				} else {
					assert.NotContains(t, response.Body.String(), "question")
				}
			}
		})
	}
}

func TestConversationWebSocketRecordsEachTurn(t *testing.T) {
	t.Setenv("CONVERSATION_LOG_ENABLED", "true")
	fixture := newResponsesWSBillingTest(t, "p", func(ws *websocket.Conn, _ *http.Request) {
		for turn := range 2 {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
			delta := fmt.Sprintf(`{"type":"response.output_text.delta","delta":"reply-%d"}`, turn)
			if err := ws.WriteMessage(websocket.TextMessage, []byte(delta)); err != nil {
				return
			}
			terminal := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d","model":"fixture-actual","status":"completed","output":[]}}`, turn)
			if err := ws.WriteMessage(websocket.TextMessage, []byte(terminal)); err != nil {
				return
			}
		}
		_, _, _ = ws.ReadMessage()
	})
	require.NoError(t, model.DB.AutoMigrate(&model.ConversationLog{}))
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.AuditLog{}))
	service.StartConversationRecorder()
	for turn := range 2 {
		payload := fmt.Sprintf(`{"type":"response.create","model":"ws-billing","input":"question-%d"}`, turn)
		require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(payload)))
		require.NoError(t, fixture.client.SetReadDeadline(time.Now().Add(5*time.Second)))
		for range 2 {
			_, _, err := fixture.client.ReadMessage()
			require.NoError(t, err)
		}
	}
	fixture.closeAndWait(t)
	require.Eventually(t, func() bool {
		var count int64
		return model.LOG_DB.Model(&model.AuditLog{}).Where("category = ?", model.AuditCategoryConversation).Count(&count).Error == nil && count == 2
	}, 5*time.Second, 10*time.Millisecond)
	var rows []model.ConversationLog
	require.NoError(t, model.DB.Order("id").Find(&rows).Error)
	require.Len(t, rows, 2)
	for turn, row := range rows {
		_, content, err := model.GetConversation(row.RequestId, 0)
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("question-%d", turn), content.LatestInput)
		assert.Equal(t, fmt.Sprintf("reply-%d", turn), content.Output)
		assert.Equal(t, "completed", row.Outcome)
		assert.Equal(t, "websocket", row.Protocol)
	}
}
