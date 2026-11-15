package middleware

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type conversationResponseWriter struct {
	gin.ResponseWriter
	recorder *service.ConversationCapture
}

func (w *conversationResponseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.recorder.Observe(data[:n])
	return n, err
}

func (w *conversationResponseWriter) WriteString(data string) (int, error) {
	n, err := w.ResponseWriter.WriteString(data)
	w.recorder.Observe([]byte(data[:n]))
	return n, err
}

func (w *conversationResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func ConversationRecording() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := strings.TrimSuffix(c.Request.URL.Path, "/")
		if c.Request.Method != http.MethodPost || (path != "/v1/chat/completions" && path != "/v1/responses" && path != "/pg/chat/completions") {
			c.Next()
			return
		}
		r := service.BeginConversation(c, "http")
		if r == nil {
			c.Next()
			return
		}
		c.Writer = &conversationResponseWriter{ResponseWriter: c.Writer, recorder: r}
		defer func() { r.Finish(c.Writer.Status(), c.Request.Context().Err() != nil) }()
		c.Next()
	}
}
