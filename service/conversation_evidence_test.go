package service

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestQAEvidencePreservesVisibleBlocksWithGaps(t *testing.T) {
	in := map[string]any{"input": []any{map[string]any{"role": "assistant", "content": "old"}, map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "<ide_opened_file>note</ide_opened_file>"}, map[string]any{"type": "not_known", "text": "excluded"}}}, map[string]any{"role": "user", "content": "task"}}}
	f := qaRequest(in, nil, "", "responses")
	require.Equal(t, "<ide_opened_file>note</ide_opened_file>\ntask", f.Prompt)
	require.False(t, f.Unclear)
}
func TestQAEvidenceLongStreamRetainsShortTerminal(t *testing.T) {
	q := qaCapture{limit: 512}
	for range 80 {
		q.write([]byte("data: {\"type\":\"response.in_progress\"}\n\n"))
	}
	q.write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}]}}\n"))
	q.finishWire()
	var f model.QAFacts
	q.result(&f, true)
	require.Equal(t, "done", f.Final)
	q = qaCapture{limit: 512}
	q.write([]byte(strings.Repeat("x", 513)))
	q.finishWire()
	require.True(t, q.overflow)
}

func TestQAEvidenceMasksCredentialsBeforePersistence(t *testing.T) {
	raw := "prefix\\nAuthorization: Bearer synthetic-example\\npassword='invented' end"

	f := qaRequest(map[string]any{"input": raw}, nil, "", "responses")
	require.NotContains(t, f.Prompt, "invented")
	require.NotContains(t, f.Prompt, "synthetic-example")
	require.Contains(t, f.Prompt, "prefix")
	require.Equal(t, "redacted", f.InputState)
}
