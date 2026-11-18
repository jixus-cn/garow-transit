package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetModelMaxContextTokens(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB; assert.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&Model{}, &Ability{}, &Vendor{}, &Channel{}, &Option{}))

	require.NoError(t, db.Create(&Model{ModelName: "gpt-4o", MaxContextTokens: 1000, NameRule: NameRuleExact, Status: 1}).Error)
	require.NoError(t, db.Create(&Model{ModelName: "gpt-", MaxContextTokens: 2000, NameRule: NameRulePrefix, Status: 1}).Error)
	require.NoError(t, db.Create(&Model{ModelName: "-vision", MaxContextTokens: 3000, NameRule: NameRuleSuffix, Status: 1}).Error)
	require.NoError(t, db.Create(&Model{ModelName: "claude", MaxContextTokens: 4000, NameRule: NameRuleContains, Status: 1}).Error)
	require.NoError(t, db.Create(&Model{ModelName: "unlimited", MaxContextTokens: 0, NameRule: NameRuleExact, Status: 1}).Error)
	RefreshPricing()

	assert.Equal(t, 1000, GetModelMaxContextTokens("gpt-4o"), "exact rule wins over prefix")
	assert.Equal(t, 2000, GetModelMaxContextTokens("gpt-4o-mini"), "prefix rule applies")
	assert.Equal(t, 3000, GetModelMaxContextTokens("gemini-vision"), "suffix rule applies")
	assert.Equal(t, 4000, GetModelMaxContextTokens("anthropic-claude-3"), "contains rule applies")
	assert.Equal(t, 0, GetModelMaxContextTokens("unlimited"), "zero means unlimited")
	assert.Equal(t, 0, GetModelMaxContextTokens("no-such-model"), "unconfigured model is unlimited")

	t.Run("update persists and refreshes the limit", func(t *testing.T) {
		var m Model
		require.NoError(t, db.Where("model_name = ?", "gpt-4o").First(&m).Error)
		m.MaxContextTokens = 272000
		require.NoError(t, m.Update())
		RefreshPricing()
		assert.Equal(t, 272000, GetModelMaxContextTokens("gpt-4o"))
		var stored Model
		require.NoError(t, db.Where("model_name = ?", "gpt-4o").First(&stored).Error)
		assert.Equal(t, 272000, stored.MaxContextTokens, "column persisted on update")
	})
}
