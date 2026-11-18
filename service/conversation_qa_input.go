package service

import (
	"github.com/QuantumNous/new-api/common"
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/model"
)

func qaString(v any) string { s, _ := v.(string); return s }
func qaID(v any) string {
	s := qaString(v)
	if len(s) == 0 || len(s) > 255 || strings.ContainsAny(s, "\r\n\x00") {
		return ""
	}
	return s
}

func qaReturned(m map[string]any) (string, bool) {
	if m["role"] == "tool" {
		return qaID(m["tool_call_id"]), true
	}
	switch m["type"] {
	case "function_call_output", "mcp_tool_call_output", "tool_search_output", "custom_tool_call_output", "computer_call_output", "local_shell_call_output", "mcp_approval_response":
		return qaID(m["call_id"]), true
	}
	return "", false
}

func qaRequest(req map[string]any, h http.Header, path, kind string) model.QAFacts {
	f := model.QAFacts{Client: kind}
	if req == nil || req["capture"] != nil {
		f.Missing = true
		return f
	}
	if nested, ok := req["response"].(map[string]any); ok {
		req = nested
	}
	aux := req["generate"] == false || h.Get("X-OpenAI-Subagent") != "" || h.Get("X-OpenAI-Memgen-Request") != ""
	controlled := path == "/pg/chat/completions" && qaID(h.Get("X-Garow-Chat-Id")) != "" && qaID(h.Get("X-Garow-Submission-Id")) != ""
	if controlled {
		f.Client = "playground"
		f.Chat = qaID(h.Get("X-Garow-Chat-Id"))
		f.Submission = qaID(h.Get("X-Garow-Submission-Id"))
	} else {
		for _, key := range []string{"conversation_id", "conversation", "session_id"} {
			val := req[key]
			if m, ok := val.(map[string]any); ok {
				val = m["id"]
			}
			if id := qaID(val); id != "" {
				f.Chat = model.ConversationHash(key + "|" + id)
				break
			}
		}
		// The Codex Responses thread-id identifies the chat. Its hyphenated
		// session-id can be used for cache routing and is deliberately ignored.
		thread := qaID(h.Get("Thread-Id"))
		product := strings.ToLower(strings.Fields(h.Get("User-Agent") + " unknown")[0])
		product, _, _ = strings.Cut(product, "/")
		if origin := qaID(h.Get("Originator")); origin != "" {
			product = strings.ToLower(origin)
		}
		if h != nil {
			f.Client = kind + "/" + product
		}
		if thread != "" {
			f.Chat = model.ConversationHash("thread_id|" + thread)
		} else {
			headers := []string{h.Get("Session_id"), h.Get("X-Codex-Session-Id")}
			for _, header := range headers {
				if id := qaID(header); id != "" {
					key := model.ConversationHash("session_id|" + id)
					if f.Chat != "" && f.Chat != key {
						f.Chat = ""
						f.Unclear = true
						break
					}
					f.Chat = key
				}
			}
		}
	}
	f.Parent = qaID(req["previous_response_id"])
	value := req["input"]
	if value == nil {
		value = req["messages"]
	}
	var parts qaInput
	switch v := value.(type) {
	case string:
		parts.add(v, 0, 0)
	case map[string]any:
		qaSuffix(&f, &parts, []any{v}, controlled)
	case []any:
		qaSuffix(&f, &parts, v, controlled)
	case nil:
		if f.Parent == "" && f.Submission == "" {
			f.Missing = true
		}
	default:
		f.Missing = true
	}
	f.Prompt = parts.text.String()
	if len(parts.spans) > 0 {
		raw, _ := common.Marshal(parts.spans)
		f.Parts = string(raw)
	}
	switch {
	case parts.partial:
		f.InputState = "partial"
	case parts.redacted:
		f.InputState = "redacted"
	case f.Prompt != "" && aux:
		f.InputState = "client_aux"
	case f.Prompt != "":
		f.InputState = "text"
	case f.Missing:
		f.InputState = "unavailable"
	case len(f.Returned) > 0:
		f.InputState = "continuation"
	default:
		f.InputState = "no_new_input"
	}
	return f
}

// Only the user-side suffix is newly observed input. Historical transcript entries
// and actual tool results are never copied into a new question.
func qaSuffix(f *model.QAFacts, parts *qaInput, items []any, controlled bool) {
	start := len(items)
	users := 0
	for i := len(items) - 1; i >= 0; i-- {
		m, ok := items[i].(map[string]any)
		if !ok {
			parts.partial = true
			break
		}
		if id, tool := qaReturned(m); tool {
			if users > 0 {
				break
			}
			if id == "" || len(f.Returned) >= 128 {
				f.Unclear = true
				return
			}
			if !slices.Contains(f.Returned, id) {
				f.Returned = append(f.Returned, id)
			}
			continue
		}
		if len(f.Returned) > 0 {
			break
		}
		switch m["role"] {
		case "user":
			users++
			start = i
			if controlled {
				i = -1
			}
		case "system", "developer":
			continue
		default:
			i = -1
		}
	}
	for i := start; i < len(items); i++ {
		m, ok := items[i].(map[string]any)
		if !ok || m["role"] != "user" {
			continue
		}
		switch v := m["content"].(type) {
		case string:
			parts.add(v, i, 0)
		case []any:
			for j, raw := range v {
				switch p := raw.(type) {
				case string:
					parts.add(p, i, j)
				case map[string]any:
					switch p["type"] {
					case "input_text", "text":
						t, ok := p["text"].(string)
						if ok {
							parts.add(t, i, j)
						} else {
							parts.gap(i, j, "unsupported")
						}
					case "image_url", "input_image", "input_file", "file", "input_audio":
						parts.gap(i, j, "attachment")
					default:
						parts.gap(i, j, "unsupported")
					}
				default:
					parts.gap(i, j, "unsupported")
				}
			}
		default:
			parts.gap(i, 0, "unsupported")
		}
	}
	if users == 0 && len(f.Returned) == 0 && f.Parent == "" && f.Submission == "" {
		f.Unclear = true
	}
}
