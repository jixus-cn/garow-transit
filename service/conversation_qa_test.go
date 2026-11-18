package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestQAInputBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, body, prompt string
		returned           []string
		unclear            bool
	}{
		{"history and IDE", `{"input":[{"role":"user","content":"old"},{"role":"assistant","content":"old reply"},{"role":"user","content":[{"type":"input_text","text":"<environment_context>workspace</environment_context>"},{"type":"input_text","text":"  new\nraw  "}]}]}`, "<environment_context>workspace</environment_context>\n  new\nraw  ", nil, false},
		{"editor heading", `{"input":"# Context from my IDE setup:\nfile.go\n## My request for Codex:\nFix this."}`, "# Context from my IDE setup:\nfile.go\n## My request for Codex:\nFix this.", nil, false},
		{"only new tools", `{"input":[{"type":"function_call_output","call_id":"old"},{"role":"assistant","content":"history"},{"type":"function_call_output","call_id":"b"},{"type":"function_call_output","call_id":"a"}]}`, "", []string{"a", "b"}, false},
		{"quote", `{"input":"Example: <environment_context>here</environment_context>"}`, "Example: <environment_context>here</environment_context>", nil, false},
		{"mixed wrapper", `{"input":"<unknown>editor</unknown> maybe a question"}`, "<unknown>editor</unknown> maybe a question", nil, false},
		{"history only", `{"input":[{"role":"user","content":"old"},{"role":"assistant","content":"answer"}]}`, "", nil, true},
		{"two unmarked prompts", `{"input":[{"role":"user","content":"first"},{"role":"user","content":"second"}]}`, "first\nsecond", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r map[string]any
			require.NoError(t, common.UnmarshalJsonStr(tc.body, &r))
			f := qaRequest(r, nil, "", "responses")
			require.Equal(t, tc.prompt, f.Prompt)
			require.Equal(t, tc.returned, f.Returned)
			require.Equal(t, tc.unclear, f.Unclear)
		})
	}
	h := http.Header{}
	h.Set("X-Garow-Chat-Id", "chat")
	h.Set("X-Garow-Submission-Id", "send")
	f := qaRequest(map[string]any{"input": "<example>literal</example>"}, h, "/pg/chat/completions", "chat")
	require.Equal(t, "<example>literal</example>", f.Prompt)
	require.Equal(t, "send", f.Submission)
}
func TestQAThreadScopeIsNotCacheOrRequestIdentity(t *testing.T) {
	h := http.Header{}
	h.Set("Originator", "codex_cli_rs")
	h.Set("Thread-Id", "A")
	h.Set("Session-Id", "shared cache")
	h.Set("X-Client-Request-Id", "transport")
	req := map[string]any{"input": "same question"}
	a := qaRequest(req, h, "/v1/responses", "responses")
	h.Set("Session-Id", "different cache")
	h.Set("X-Client-Request-Id", "another request")
	reopened := qaRequest(req, h, "/v1/responses", "responses")
	require.NotEmpty(t, a.Chat)
	require.Equal(t, a.Chat, reopened.Chat)
	h.Set("Thread-Id", "B")
	require.NotEqual(t, a.Chat, qaRequest(req, h, "/v1/responses", "responses").Chat)
	h.Del("Thread-Id")
	require.Empty(t, qaRequest(req, h, "/v1/responses", "responses").Chat)
	h.Set("X-OpenAI-Subagent", "review")
	require.Equal(t, "client_aux", qaRequest(req, h, "/v1/responses", "responses").InputState)
}
func TestQATerminalEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, wire, answer string
		ok, ws             bool
	}{
		{"completed final", `{"status":"completed","output":[{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"checking"}]},{"type":"message","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"  exact\n"},{"type":"output_text","text":"reply "}]}]}`, "  exact\nreply ", true, false},
		{"stop stream", "data: {\"id\":\"a\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"abc\"}}]}\n\ndata: {\"id\":\"a\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n", "abc", true, false},
		{"done only", "data: {\"choices\":[{\"delta\":{\"content\":\"unfinished\"}}]}\ndata: [DONE]\n", "", true, false},
		{"normal chat", `{"choices":[{"message":{"role":"assistant","content":"plain"},"finish_reason":"stop"}]}`, "plain", true, false},
		{"length", `{"choices":[{"message":{"content":"partial"},"finish_reason":"length"}]}`, "", true, false},
		{"multiple", `{"choices":[{"message":{"content":"A"},"finish_reason":"stop"},{"message":{"content":"B"},"finish_reason":"stop"}]}`, "", true, false},
		{"tool", `{"choices":[{"message":{"content":"looking","tool_calls":[{"id":"tool"}]},"finish_reason":"tool_calls"}]}`, "", true, false},
		{"unphased", `{"status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"decline"}]}]}`, "decline", true, false},
		{"two unphased", `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"first"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"second"}]}]}`, "", true, false},
		{"write cancelled", `{"choices":[{"message":{"content":"plain"},"finish_reason":"stop"}]}`, "", false, false},
		{"websocket", `{"type":"response.output_item.done","output_index":1,"item":{"type":"message","phase":"final_answer","role":"assistant","content":[{"type":"output_text","text":"ws answer"}]}}
{"type":"response.completed","response":{"id":"r","status":"completed","output":[]}}
`, "ws answer", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := qaCapture{limit: 1 << 20, ws: tc.ws}
			for _, b := range []byte(tc.wire) {
				q.write([]byte{b})
			}
			q.finishWire()
			var f model.QAFacts
			q.result(&f, tc.ok)
			require.Equal(t, tc.answer, f.Final)
		})
	}
}
