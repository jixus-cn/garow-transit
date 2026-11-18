package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestConversationVisibleSources(t *testing.T) {
	v := visibleConversationOutput{}
	for _, s := range []string{`{"type":"response.output_item.added","output_index":1,"item":{"id":"b","type":"message","role":"assistant","phase":"final_answer"}}`, `{"type":"response.output_text.delta","item_id":"b","delta":"end"}`, `{"type":"response.output_item.added","output_index":0,"item":{"id":"a","type":"message","role":"assistant","phase":"commentary"}}`, `{"type":"response.output_text.delta","item_id":"a","delta":"checking"}`, `{"type":"response.output_text.done","item_id":"b","text":"end"}`} {
		var ev map[string]any
		require.NoError(t, common.UnmarshalJsonStr(s, &ev))
		v.observe(ev)
	}
	parts := v.parts("completed", false)
	require.Len(t, parts, 2)
	require.Equal(t, "checking", parts[0].Text)
	require.Equal(t, "commentary", parts[0].Kind)
	require.Equal(t, "final", parts[1].Kind)
	require.Equal(t, "partial", v.parts("interrupted", false)[1].Kind)
	old := &model.ConversationContent{Request: `{"input":[{"role":"user","content":"actual question"},{"role":"user","content":"<environment_context>fixture</environment_context>"}]}`, Response: `[{"type":"response.reasoning_summary_text.done","text":"fixture summary"}]`, Output: "fixture summary"}
	require.NoError(t, ApplyConversationReading(&model.ConversationLog{Outcome: "interrupted"}, old))
	require.Equal(t, "actual question", old.LatestInput)
	require.Empty(t, old.Output)
	require.Empty(t, old.Reading.Replies)
	for _, input := range []string{`{"input":"<environment_context>fixture</environment_context>"}`, `{"input":[{"role":"user","content":"<environment_context>fixture</environment_context>"}]}`} {
		old.Request = input
		require.NoError(t, ApplyConversationReading(&model.ConversationLog{}, old))
		require.Empty(t, old.LatestInput)
		require.Equal(t, "context", old.Reading.Input[0].Kind)
	}
}
