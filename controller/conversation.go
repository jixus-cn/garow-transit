package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

// conversationRootSession gates every session-audit endpoint: a live root
// dashboard session, never an API/PAT token. Bodies stay no-store.
func conversationRootSession(c *gin.Context) bool {
	if _, ok := middleware.GetSessionAuthIdentity(c); !ok || c.GetInt("role") != common.RoleRootUser {
		c.AbortWithStatus(http.StatusForbidden)
		return false
	}
	c.Header("Cache-Control", "no-store")
	return true
}

func conversationTimeRange(c *gin.Context) (int64, int64, bool) {
	var start, end int64
	for name, target := range map[string]*int64{"start_timestamp": &start, "end_timestamp": &end} {
		if raw := c.Query(name); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				common.ApiErrorMsg(c, "Invalid conversation time range")
				return 0, 0, false
			}
			*target = parsed
		}
	}
	if end > 0 && end < start {
		common.ApiErrorMsg(c, "Invalid conversation time range")
		return 0, 0, false
	}
	return start, end, true
}

func GetConversationSessions(c *gin.Context) {
	if conversationRootSession(c) {
		qaList(c, "chats")
	}
}
func GetConversationSessionDetail(c *gin.Context) {
	if conversationRootSession(c) {
		qaList(c, "chat")
	}
}
