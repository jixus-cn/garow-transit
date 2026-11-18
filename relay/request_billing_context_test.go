package relay

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupContextLimitModel(t *testing.T, limit int) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB; assert.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.Model{}, &model.Ability{}, &model.Vendor{}, &model.Channel{}, &model.Option{}))
	require.NoError(t, db.Create(&model.Model{ModelName: "gpt-4o", MaxContextTokens: limit, NameRule: model.NameRuleExact, Status: 1}).Error)
	model.RefreshPricing()
}

func contextLimitRequest(t *testing.T, words int) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-4o")
	req := &dto.GeneralOpenAIRequest{}
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"` + strings.Repeat("token ", words) + `"}]}`
	require.NoError(t, common.Unmarshal([]byte(body), req))
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4o",
		RelayFormat:     types.RelayFormatOpenAI,
		Request:         req,
	}
	return c, info
}

func TestEnforceModelContextLimit(t *testing.T) {
	t.Run("over limit is rejected", func(t *testing.T) {
		setupContextLimitModel(t, 5)
		c, info := contextLimitRequest(t, 200)
		apiErr := EnforceModelContextLimit(c, info, info.Request.GetTokenCountMeta())
		require.NotNil(t, apiErr)
		assert.Equal(t, types.ErrorCodeContextLengthExceeded, apiErr.GetErrorCode())
		assert.Equal(t, 400, apiErr.StatusCode)
	})

	t.Run("under limit passes", func(t *testing.T) {
		setupContextLimitModel(t, 1000000)
		c, info := contextLimitRequest(t, 50)
		assert.Nil(t, EnforceModelContextLimit(c, info, info.Request.GetTokenCountMeta()))
	})

	t.Run("unconfigured model is never rejected", func(t *testing.T) {
		setupContextLimitModel(t, 0)
		c, info := contextLimitRequest(t, 5000)
		assert.Nil(t, EnforceModelContextLimit(c, info, info.Request.GetTokenCountMeta()))
	})
}
