package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

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
