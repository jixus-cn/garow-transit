package service

import (
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

// A wrapper is evidence only at a content boundary. Never search a user's code
// or prose for instructions. The playground supplies an explicit user boundary.
func qaUserPart(s string, controlled bool) (string, bool) {
	if controlled {
		return s, true
	}
	lead := strings.TrimLeft(s, " \t\r\n")
	for _, name := range []string{"user_query", "user_request", "task"} {
		if inner, ok := strings.CutPrefix(lead, "<"+name+">"); ok {
			inner, after, closed := strings.Cut(inner, "</"+name+">")
			if !closed || strings.TrimSpace(after) != "" {
				return "", false
			}
			inner = strings.TrimPrefix(strings.TrimPrefix(inner, "\r"), "\n")
			return strings.TrimSuffix(strings.TrimSuffix(inner, "\n"), "\r"), true
		}
	}
	for _, prefix := range []string{"Another language model started to solve this problem and produced a summary", "This session is being continued from a previous conversation that ran out of context", "<user_action>", "<command-name>", "<local-command-", "<system-reminder", "<environment_details", "<environment_context", "<subagent_notification", "<permissions instructions", "<app-context", "<ide_opened_file", "<ide_selection", "<user_instructions", "<skills_instructions"} {
		if strings.HasPrefix(lead, prefix) {
			// A known complete structural wrapper can precede a real submission in
			// the same text part. The common splitter handles that case below.
			if !strings.Contains(lead, "</") || !strings.HasPrefix(prefix, "<") {
				return "", true
			}
			break
		}
	}
	parts := model.ConversationQAParts(s)
	if strings.HasPrefix(lead, "<") && len(parts) > 0 && parts[0].Kind == "text" && strings.Contains(strings.SplitN(lead, "\n", 2)[0], ">") {
		return "", false
	}
	var keep []string
	for _, p := range parts {
		switch p.Kind {
		case "unknown":
			return "", false
		case "text":
			keep = append(keep, p.Text)
		}
	}
	return strings.Join(keep, "\n"), true
}

func qaUserContent(content any, controlled bool) (string, bool) {
	var values []string
	switch v := content.(type) {
	case string:
		values = []string{v}
	case []any:
		for _, r := range v {
			switch p := r.(type) {
			case string:
				values = append(values, p)
			case map[string]any:
				switch p["type"] {
				case "input_text", "text":
					values = append(values, qaString(p["text"]))
				case "image_url", "input_image", "input_file", "file", "input_audio":
					continue
				default:
					return "", false
				}
			default:
				return "", false
			}
		}
	default:
		return "", false
	}
	var texts []string
	for _, s := range values {
		t, known := qaUserPart(s, controlled)
		if !known {
			return "", false
		}
		if strings.TrimSpace(t) != "" {
			texts = append(texts, t)
		}
	}
	return strings.Join(texts, "\n"), true
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
	if req["generate"] == false || h.Get("X-OpenAI-Subagent") != "" || h.Get("X-OpenAI-Memgen-Request") != "" {
		f.Missing = true
		return f
	}
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
	switch v := value.(type) {
	case string:
		f.Prompt, _ = qaUserContent(v, controlled)
		if f.Prompt == "" {
			f.Unclear = true
		}
	case map[string]any:
		qaSuffix(&f, []any{v}, controlled)
	case []any:
		qaSuffix(&f, v, controlled)
	case nil:
		if f.Parent == "" && f.Submission == "" {
			f.Missing = true
		}
	default:
		f.Missing = true
	}
	if len(f.Prompt) > 1<<20 {
		f.Prompt = ""
		f.Missing = true
	}
	return f
}

// Only the suffix after the last assistant/tool boundary can be a new send.
// A replay ending in a tool result must never resurrect an earlier question.
func qaSuffix(f *model.QAFacts, items []any, controlled bool) {
	if len(items) == 0 {
		return
	}
	var prompts []string
	for i := len(items) - 1; i >= 0; i-- {
		m, ok := items[i].(map[string]any)
		if !ok {
			f.Unclear = true
			return
		}
		if id, tool := qaReturned(m); tool {
			if len(prompts) > 0 {
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
			s, known := qaUserContent(m["content"], controlled)
			if !known {
				f.Unclear = true
				return
			}
			if s != "" {
				prompts = append(prompts, s)
				if controlled {
					f.Prompt = s
					return
				}
			}
			continue
		case "system", "developer":
			continue
		}
		break
	}
	if len(prompts) == 1 {
		f.Prompt = prompts[0]
	} else if len(prompts) > 1 {
		f.Unclear = true
	}
	if len(f.Returned) == 0 && f.Prompt == "" {
		f.Unclear = true
	}
}
