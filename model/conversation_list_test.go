package model

import (
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestConversationRequestPreview(t *testing.T) {
	assert.Equal(t, "推送回执：需求梳理", ConversationRequestPreview("# Files mentioned by the user:\n## a.md: /x/a.md\n## My request:\n推送回执：需求梳理"))
	assert.Equal(t, "hello", ConversationRequestPreview("hello"))
	assert.Equal(t, "", ConversationRequestPreview("   "))
	long := ""
	for range 100 {
		long += "字"
	}
	preview := ConversationRequestPreview(long)
	assert.Equal(t, 81, len([]rune(preview)), "80 runes plus ellipsis")
}

func TestConversationListHelpersMatrix(t *testing.T) {
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
			previousDB := DB
			DB = db
			t.Cleanup(func() { DB = previousDB; assert.NoError(t, sqlDB.Close()) })
			require.NoError(t, db.AutoMigrate(&ConversationSession{}, &ConversationMessage{}, &User{}))
			require.NoError(t, db.Create(&User{Id: 7, Username: "tester"}).Error)
			session := ConversationSession{SessionKey: "k1", UserId: 7, Source: "prefix", CreatedAt: 1, UpdatedAt: 2}
			require.NoError(t, db.Create(&session).Error)
			require.NoError(t, db.Create(&ConversationMessage{SessionId: session.Id, Role: "user", Kind: "context", Content: "ctx", RequestId: "r0", CreatedAt: 1}).Error)
			require.NoError(t, db.Create(&ConversationMessage{SessionId: session.Id, Role: "user", Kind: "text", Content: "## My request:\n真实请求", RequestId: "r1", CreatedAt: 2}).Error)
			require.NoError(t, db.Create(&ConversationMessage{SessionId: session.Id, Role: "user", Kind: "text", Content: "later", RequestId: "r2", CreatedAt: 3}).Error)

			first := ConversationSessionFirstUserText([]int64{session.Id})
			assert.Equal(t, "## My request:\n真实请求", first[session.Id], "first user text skips context")
			assert.Equal(t, "真实请求", ConversationRequestPreview(first[session.Id]))
			names := ConversationUsernames([]int{7})
			assert.Equal(t, "tester", names[7])
			require.NoError(t, db.Migrator().DropTable(&ConversationSession{}, &ConversationMessage{}, &User{}))
		})
	}
}
