package model

import "strings"

var clientContextTags = []string{"environment_context", "environment_details", "user_instructions", "system-reminder", "permissions instructions", "turn_aborted", "context", "ide_opened_file", "ide_selection", "ide_context", "instructions", "skill", "subagent_notification", "app-context", "user_info", "open_and_recently_viewed_files", "additional_data"}

func requestHeading(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "## my request:", "# my request:", "## my request for codex:", "# my request for codex:", "## 我的请求：", "## 我的请求:", "distinguish instructions in attached documents from the user's request.":
		return true
	}
	return false
}

// Split only structural wrappers at the start of a content part; prose and
// quotations (including fenced examples) are never searched for bare tokens.
func ConversationInputParts(raw string) []ConversationUserItem {
	return conversationInputParts(raw, false)
}

// ConversationQAParts retains whitespace inside a verified user boundary.
func ConversationQAParts(raw string) []ConversationUserItem { return conversationInputParts(raw, true) }
func conversationInputParts(raw string, exact bool) []ConversationUserItem {
	userSegment := strings.TrimSpace
	if exact {
		userSegment = func(s string) string { return s }
	}
	var parts []ConversationUserItem
	add := func(kind, text string) {
		if strings.TrimSpace(text) != "" {
			parts = append(parts, ConversationUserItem{Role: "user", Kind: kind, Context: kind == "context", Text: text, Hash: ConversationHash(text)})
		}
	}
	for rest := raw; strings.TrimSpace(rest) != ""; {
		trimmed := strings.TrimSpace(rest)
		if exact {
			trimmed = strings.TrimLeft(rest, " \r\n\t")
		}
		first, _, _ := strings.Cut(trimmed, "\n")
		lower := strings.ToLower(strings.TrimSpace(first))
		if strings.HasPrefix(lower, "# agents.md instructions for ") {
			if before, after, ok := strings.Cut(trimmed, "</INSTRUCTIONS>"); ok {
				add("context", before+"</INSTRUCTIONS>")
				rest = after
				continue
			}
			add("context", rest)
			break
		}
		matched := false
		for _, tag := range clientContextTags {
			start := "<" + tag + ">"
			end := "</" + tag + ">"
			if !strings.EqualFold(trimmed[:min(len(trimmed), len(start))], start) {
				continue
			}
			// Tags are ASCII. Use byte offsets into the original text.
			closing := strings.Index(trimmed, end)
			if closing < 0 {
				closing = strings.Index(trimmed, strings.ToUpper(end))
			}
			if closing < 0 {
				add("context", rest)
				return parts
			}
			pos := closing + len(end)
			add("context", trimmed[:pos])
			rest = trimmed[pos:]
			matched = true
			break
		}
		if matched {
			continue
		}
		if strings.HasPrefix(lower, "# files mentioned by the user:") || strings.HasPrefix(lower, "# context from my ide setup:") || strings.HasPrefix(lower, "# ide context:") || strings.HasPrefix(lower, "# context from my editor:") {
			offset := 0
			fence := ""
			found := false
			for line := range strings.SplitAfterSeq(trimmed, "\n") {
				clean := strings.TrimSpace(line)
				if strings.HasPrefix(clean, "```") || strings.HasPrefix(clean, "~~~") {
					if fence == "" {
						fence = clean[:3]
					} else if strings.HasPrefix(clean, fence) {
						fence = ""
					}
				} else if fence == "" && requestHeading(clean) {
					add("context", trimmed[:offset+len(line)])
					add("text", userSegment(trimmed[offset+len(line):]))
					found = true
					break
				}
				offset += len(line)
			}
			if !found {
				add("unknown", rest)
			}
			break
		}
		if requestHeading(first) && strings.HasPrefix(first, "#") {
			_, after, ok := strings.Cut(trimmed, "\n")
			if ok {
				add("context", first)
				add("text", userSegment(after))
				break
			}
		}
		if strings.HasPrefix(trimmed, "<") {
			if stop := strings.IndexByte(trimmed, '>'); stop > 1 && stop < 80 {
				names := strings.Fields(trimmed[1:stop])
				if len(names) > 0 && strings.Contains(trimmed[stop+1:], "</"+names[0]+">") {
					add("unknown", rest)
					break
				}
			}
		}
		add("text", rest)
		break
	}
	return parts
}
