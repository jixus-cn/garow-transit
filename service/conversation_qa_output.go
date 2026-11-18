package service

import (
	"bytes"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Only one wire line (or one bounded JSON response) and confirmed message
// candidates are retained in memory. Commentary, arguments and tool results
// are never sent to the writer.
type qaCapture struct {
	facts                                                                       model.QAFacts
	limit, candidateBytes                                                       int
	ws, decided, framed, overflow, invalid, failed, terminal, hasTool, chatStop bool
	buf                                                                         []byte
	responseID, final                                                           string
	chat                                                                        strings.Builder
	issued                                                                      []string
	doneItems                                                                   map[int]map[string]any
}

func (q *qaCapture) write(p []byte) {
	if q.overflow {
		return
	}
	if !q.decided && !q.ws {
		first := bytes.TrimLeft(p, " \t\n\r")
		if len(first) == 0 {
			return
		}
		q.decided = true
		q.framed = first[0] != '{' && first[0] != '['
	}
	if !q.framed && !q.ws {
		if len(q.buf)+len(p) > q.limit {
			q.overflow = true
			q.buf = nil
			return
		}
		q.buf = append(q.buf, p...)
		return
	}
	for len(p) > 0 {
		line, rest, ok := bytes.Cut(p, []byte{'\n'})
		if len(line)+len(q.buf) > q.limit {
			q.overflow = true
			q.buf = nil
			return
		}
		q.buf = append(q.buf, line...)
		if !ok {
			return
		}
		q.line(q.buf)
		q.buf = q.buf[:0]
		p = rest
	}
}

func (q *qaCapture) finishWire() {
	if len(bytes.TrimSpace(q.buf)) != 0 {
		q.line(q.buf)
	}
	q.buf = nil
}
func (q *qaCapture) line(p []byte) {
	b := bytes.TrimSpace(p)
	if len(b) == 0 {
		return
	}
	if q.framed && !q.ws {
		var ok bool
		b, ok = bytes.CutPrefix(b, []byte("data:"))
		if !ok {
			return
		}
		b = bytes.TrimSpace(b)
		if bytes.Equal(b, []byte("[DONE]")) {
			return
		}
	}
	var ev map[string]any
	if common.Unmarshal(b, &ev) != nil || ev == nil {
		q.invalid = true
		return
	}
	q.event(ev)
}
func (q *qaCapture) identity(id string) {
	if id == "" {
		return
	}
	if q.responseID != "" && q.responseID != id {
		q.invalid = true
	}
	q.responseID = id
}
func (q *qaCapture) tool(id string) {
	q.hasTool = true
	if id != "" && !slices.Contains(q.issued, id) {
		if len(q.issued) >= 128 {
			q.invalid = true
			return
		}
		q.issued = append(q.issued, id)
	}
}

// Exactly one complete message is eligible. An explicit final may follow commentary.
// Without a phase, require a unique assistant message in a terminal Responses output.
func qaTerminalMessage(out []any) (string, bool) {
	explicit, unphased, messages := 0, 0, 0
	var answer, fallback string
	for _, raw := range out {
		m, ok := raw.(map[string]any)
		if !ok {
			return "", false
		}
		if m["type"] != "message" {
			continue
		}
		if m["role"] != "assistant" {
			return "", false
		}
		messages++
		phase := qaString(m["phase"])
		if phase == "commentary" {
			continue
		}
		if (phase != "" && phase != "final_answer") || (m["status"] != nil && m["status"] != "completed") {
			return "", false
		}
		var b strings.Builder
		parts, _ := m["content"].([]any)
		for _, raw := range parts {
			p, _ := raw.(map[string]any)
			switch p["type"] {
			case "output_text":
				b.WriteString(qaString(p["text"]))
			case "refusal":
				b.WriteString(qaString(p["refusal"]))
			default:
				return "", false
			}
		}
		if phase == "final_answer" {
			explicit++
			answer = b.String()
		} else {
			unphased++
			fallback = b.String()
		}
	}
	if explicit == 1 && unphased == 0 {
		return answer, true
	}
	if explicit == 0 && unphased == 1 && messages == 1 {
		return fallback, true
	}
	return "", false
}
func (q *qaCapture) observeTool(m map[string]any) {
	typ := qaString(m["type"])
	// Hosted searches, reasoning and their arguments are not terminal text; only
	// calls requiring client execution leave outstanding edges.
	switch typ {
	case "function_call", "custom_tool_call", "local_shell_call", "computer_call", "mcp_approval_request":
		q.tool(qaID(m["call_id"]))
	}
}
func (q *qaCapture) event(e map[string]any) {
	typ := qaString(e["type"])
	if e["error"] != nil || typ == "error" || typ == "response.failed" || typ == "response.incomplete" || typ == "response.cancelled" || typ == "response.canceled" {
		q.failed = true
		return
	}
	if xs, ok := e["choices"].([]any); ok {
		q.identity(qaID(e["id"]))
		if len(xs) == 0 {
			return
		}
		if len(xs) != 1 {
			q.invalid = true
			return
		}
		c, _ := xs[0].(map[string]any)
		if eventInt(c["index"], 0) != 0 {
			q.invalid = true
			return
		}
		m, full := c["message"].(map[string]any)
		if !full {
			m, _ = c["delta"].(map[string]any)
		}
		if role := qaString(m["role"]); role != "" && role != "assistant" {
			q.invalid = true
			return
		}
		if m["function_call"] != nil {
			q.tool("")
		}
		calls, _ := m["tool_calls"].([]any)
		for _, r := range calls {
			v, _ := r.(map[string]any)
			q.tool(qaID(v["id"]))
		}
		var s string
		switch v := m["content"].(type) {
		case string:
			s = v
		case nil:
		default:
			q.invalid = true
		}
		s += qaString(m["refusal"])
		if full {
			if q.chat.Len() > 0 && q.chat.String() != s {
				q.invalid = true
			}
			q.chat.Reset()
		} else if q.terminal && s != "" {
			q.invalid = true
		}
		if q.chat.Len()+len(s) > qaTextLimit {
			q.overflow = true
			q.chat.Reset()
			return
		}
		q.chat.WriteString(s)
		if reason := qaString(c["finish_reason"]); reason != "" {
			if q.terminal && q.chatStop != (reason == "stop") {
				q.invalid = true
			}
			q.terminal = true
			q.chatStop = reason == "stop"
			if reason == "tool_calls" || reason == "function_call" {
				q.hasTool = true
			}
		}
		return
	}
	if m, ok := e["item"].(map[string]any); ok && (typ == "response.output_item.added" || typ == "response.output_item.done") {
		q.observeTool(m)
		if typ == "response.output_item.done" && m["type"] == "message" {
			i := eventInt(e["output_index"], -1)
			if i < 0 {
				return
			}
			if q.doneItems == nil {
				q.doneItems = map[int]map[string]any{}
			}
			if len(q.doneItems) > 128 {
				q.invalid = true
				return
			}
			// Only complete final/unphased messages may be fallback candidates.
			if m["phase"] == "commentary" {
				q.doneItems[i] = map[string]any{"type": "message", "role": "assistant", "phase": "commentary"}
			} else {
				answer, eligible := qaTerminalMessage([]any{m})
				if !eligible {
					q.doneItems[i] = map[string]any{"type": "message", "role": "invalid"}
				} else {
					if old := q.doneItems[i]; old != nil {
						previous, _ := qaTerminalMessage([]any{old})
						q.candidateBytes -= len(previous)
					}
					q.candidateBytes += len(answer)
					if q.candidateBytes > qaTextLimit {
						q.overflow = true
						q.doneItems = nil
						return
					}
					q.doneItems[i] = map[string]any{"type": "message", "role": "assistant", "phase": m["phase"], "content": []any{map[string]any{"type": "output_text", "text": answer}}}
				}
			}
		}
	}
	resp, nested := e["response"].(map[string]any)
	if !nested {
		resp = e
	}
	if nested || typ == "" {
		q.identity(qaID(resp["id"]))
	}
	if typ != "" && typ != "response.completed" && typ != "response.done" {
		return
	}
	if resp["status"] != "completed" || resp["error"] != nil || resp["incomplete_details"] != nil {
		if resp["status"] == "failed" || resp["status"] == "incomplete" {
			q.failed = true
		}
		return
	}
	out, has := resp["output"].([]any)
	for _, r := range out {
		m, _ := r.(map[string]any)
		q.observeTool(m)
	}
	if len(out) == 0 && (typ == "response.completed" || typ == "response.done") && len(q.doneItems) > 0 {
		out = nil
		for _, m := range q.doneItems {
			out = append(out, m)
		}
		has = true
	}
	if !has {
		return
	}
	final, _ := qaTerminalMessage(out)
	if q.terminal && q.final != final {
		q.invalid = true
	}
	q.terminal = true
	q.final = final
}
func (q *qaCapture) result(f *model.QAFacts, allowed bool) {
	switch {
	case !allowed:
		f.OutputState = "http_error"
		return
	case q.failed:
		f.OutputState = "upstream_failed"
		return
	case q.overflow:
		f.OutputState = "output_limit"
		return
	case q.invalid:
		f.OutputState = "invalid_output"
		return
	case !q.terminal:
		f.OutputState = "no_terminal"
		return
	}
	f.Response = q.responseID
	f.Issued = q.issued
	if q.hasTool {
		f.OutputState = "continuation"
		return
	}
	s := q.final
	if q.chatStop {
		s = q.chat.String()
	}
	switch {
	case len(s) > qaTextLimit:
		f.OutputState = "output_limit"
	case strings.TrimSpace(s) == "":
		f.OutputState = "no_final"
	default:
		var masked bool
		f.Final, masked = model.MaskQACredentials(s)
		f.OutputState = "final"
		if masked {
			f.OutputState = "final_redacted"
		}
	}

}

func eventInt(v any, otherwise int) int {
	if n, ok := v.(float64); ok {
		return int(n)
	}
	return otherwise
}
