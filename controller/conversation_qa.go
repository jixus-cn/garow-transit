package controller

import (
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func qaList(c *gin.Context, kind string) {
	p := common.GetPageQuery(c)
	if p.Page < 1 || p.Page > 1000000 || p.PageSize < 1 || p.PageSize > 100 {
		common.ApiErrorMsg(c, "Invalid conversation pagination")
		return
	}
	f := model.QAFilter{Cutoff: model.ConversationCutoff()}
	var ok bool
	f.Start, f.End, ok = conversationTimeRange(c)
	if !ok {
		return
	}
	if raw := c.Query("user_id"); raw != "" {
		u, err := strconv.Atoi(raw)
		if err != nil || u < 0 {
			common.ApiErrorMsg(c, "Invalid conversation filters")
			return
		}
		f.User = u
	}
	var rows any
	var total int64
	var err error
	switch kind {
	case "chats":
		rows, total, err = model.QAChats(f, p.Page, p.PageSize)
	case "search":
		if c.Request.Method == http.MethodPost {
			var body struct {
				Query string `json:"q"`
			}
			if c.ShouldBindJSON(&body) != nil {
				common.ApiErrorMsg(c, "Invalid conversation search keyword")
				return
			}
			f.Query = strings.TrimSpace(body.Query)
		} else {
			f.Query = strings.TrimSpace(c.Query("q"))
		}
		if len(f.Query) == 0 || len(f.Query) > 200 {
			common.ApiErrorMsg(c, "Invalid conversation search keyword")
			return
		}
		rows, total, err = model.QATurns(f, "*", p.Page, p.PageSize)
	case "unassigned":
		rows, total, err = model.QATurns(f, "", p.Page, p.PageSize)
	default:
		key := c.Param("id")
		if _, e := hex.DecodeString(key); e != nil || len(key) != 64 {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		rows, total, err = model.QATurns(f, key, p.Page, p.PageSize)
		if err == nil && total == 0 {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
	}
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	common.ApiSuccess(c, gin.H{"items": rows, "total": total, "page": p.Page, "page_size": p.PageSize})
}
func GetUnassignedQA(c *gin.Context) {
	if conversationRootSession(c) {
		qaList(c, "unassigned")
	}
}
func SearchQA(c *gin.Context) {
	if conversationRootSession(c) {
		qaList(c, "search")
	}
}
