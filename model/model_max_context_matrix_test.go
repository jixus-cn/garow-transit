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

func TestModelMaxContextMigrationMatrix(t *testing.T) {
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

			for range 2 {
				require.NoError(t, db.AutoMigrate(&Model{}))
			}
			m := Model{ModelName: "ctx-model", MaxContextTokens: 272000, NameRule: NameRuleExact, Status: 1}
			require.NoError(t, db.Create(&m).Error)
			var stored Model
			require.NoError(t, db.Where("model_name = ?", "ctx-model").First(&stored).Error)
			assert.Equal(t, 272000, stored.MaxContextTokens)
			require.NoError(t, db.Model(&Model{}).Where("id = ?", m.Id).Update("max_context_tokens", 128000).Error)
			require.NoError(t, db.Where("model_name = ?", "ctx-model").First(&stored).Error)
			assert.Equal(t, 128000, stored.MaxContextTokens)
			require.NoError(t, db.Migrator().DropTable(&Model{}))
		})
	}
}
