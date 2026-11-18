package controller

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestConversationAttribution(t *testing.T) {
	db := sessionCaptureFixture(t)
	t.Setenv("CONVERSATION_LOG_PAYLOAD", "legacy")
	runSessionCapture(t, "parts", `{"conversation_id":"a","prompt_cache_key":"shared","input":[{"role":"user","content":[{"type":"input_text","text":"<environment_context>fixture</environment_context>"},{"type":"input_text","text":"Which tests failed?"}]},{"role":"user","content":"# AGENTS.md instructions for demo\n<INSTRUCTIONS>fixture</INSTRUCTIONS>"}]}`, sessionResponseText("part1"), false)
	var ms []model.ConversationMessage
	require.NoError(t, db.Where("request_id = ?", "parts").Order("id").Find(&ms).Error)
	var asks []string
	for _, m := range ms {
		if m.Role == "user" && m.Kind == "text" {
			asks = append(asks, m.Content)
		}
	}
	require.Equal(t, []string{"Which tests failed?"}, asks)
	runSessionCapture(t, "follow", `{"previous_response_id":"resp-part1","input":"Follow-up"}`, sessionResponseText("part2"), false)
	runSessionCapture(t, "separate", `{"conversation_id":"b","prompt_cache_key":"shared","input":"Which tests failed?"}`, sessionResponseText("part3"), false)
	var a, b, c model.ConversationLog
	require.NoError(t, db.Where("request_id = ?", "parts").First(&a).Error)
	require.NoError(t, db.Where("request_id = ?", "follow").First(&b).Error)
	require.NoError(t, db.Where("request_id = ?", "separate").First(&c).Error)
	require.Equal(t, a.SessionKey, b.SessionKey)
	require.NotEqual(t, a.SessionKey, c.SessionKey)
}
