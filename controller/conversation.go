package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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
	if !conversationRootSession(c) {
		return
	}
	page := common.GetPageQuery(c)
	if page.Page < 1 || page.PageSize < 1 || page.PageSize > 100 {
		common.ApiErrorMsg(c, "Invalid conversation pagination")
		return
	}
	userId := 0
	if raw := c.Query("user_id"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			common.ApiErrorMsg(c, "Invalid conversation filters")
			return
		}
		userId = parsed
	}
	start, end, ok := conversationTimeRange(c)
	if !ok {
		return
	}
	sessions, total, err := model.GetConversationSessions(userId, start, end, page.Page, page.PageSize)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	common.ApiSuccess(c, gin.H{"items": sessions, "total": total, "page": page.Page, "page_size": page.PageSize})
}

func GetConversationSessionDetail(c *gin.Context) {
	if !conversationRootSession(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		common.ApiErrorMsg(c, "Invalid conversation session")
		return
	}
	beforeId := int64(0)
	if raw := c.Query("before_id"); raw != "" {
		beforeId, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || beforeId < 0 {
			common.ApiErrorMsg(c, "Invalid conversation cursor")
			return
		}
	}
	limit := 200
	if raw := c.Query("page_size"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 500 {
			common.ApiErrorMsg(c, "Invalid conversation pagination")
			return
		}
	}
	session, messages, ledger, hasMore, err := model.GetConversationSessionDetail(id, beforeId, limit)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	model.RecordAuditLog(c, model.AuditLog{
		UserId: c.GetInt("id"), Username: c.GetString("username"), ActorRole: c.GetInt("role"),
		Category: model.AuditCategoryConversation, Action: "conversation.session_view",
		Content: "Conversation session · #" + strconv.FormatInt(id, 10), Success: true,
	})
	common.ApiSuccess(c, gin.H{"session": session, "messages": messages, "ledger": ledger, "has_more": hasMore})
}

func SearchConversationMessages(c *gin.Context) {
	if !conversationRootSession(c) {
		return
	}
	keyword := strings.TrimSpace(c.Query("q"))
	if keyword == "" || len(keyword) > 200 {
		common.ApiErrorMsg(c, "Invalid conversation search keyword")
		return
	}
	userId := 0
	if raw := c.Query("user_id"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			common.ApiErrorMsg(c, "Invalid conversation filters")
			return
		}
		userId = parsed
	}
	start, end, ok := conversationTimeRange(c)
	if !ok {
		return
	}
	limit := 50
	if raw := c.Query("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 50 {
			common.ApiErrorMsg(c, "Invalid conversation pagination")
			return
		}
		limit = parsed
	}
	messages, err := model.SearchConversationMessages(keyword, userId, start, end, limit)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	common.ApiSuccess(c, gin.H{"items": messages})
}

func GetConversationRequest(c *gin.Context) {
	if !conversationRootSession(c) {
		return
	}
	entry, err := model.GetConversationRequest(c.Param("request_id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	common.ApiSuccess(c, entry)
}

func GetConversation(c *gin.Context) {
	// Content requires a live root dashboard session, never an API/PAT token.
	if _, ok := middleware.GetSessionAuthIdentity(c); !ok || c.GetInt("role") != common.RoleRootUser {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	entry, content, err := model.GetConversation(c.Param("request_id"), model.ConversationCutoff())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, gin.H{"record": entry, "content": content})
}

func GetConversationThread(c *gin.Context) {
	if _, ok := middleware.GetSessionAuthIdentity(c); !ok || c.GetInt("role") != common.RoleRootUser {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	thread, err := model.GetConversationThread(c.Param("request_id"), model.ConversationCutoff())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, thread)
}
