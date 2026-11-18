package service

import (
	"cmp"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"slices"
	"strings"
)

type outputPosition struct{ item, part int }
type visibleSlot struct {
	text strings.Builder
}
type visibleConversationOutput struct {
	slots     map[outputPosition]*visibleSlot
	phases    map[int]string
	ids       map[string]int
	tools     map[int]bool
	sawEvents bool
}

func eventInt(v any, otherwise int) int {
	if n, ok := v.(float64); ok {
		return int(n)
	}
	return otherwise
}
func (v *visibleConversationOutput) slot(index, part int) *visibleSlot {
	if v.slots == nil {
		v.slots = map[outputPosition]*visibleSlot{}
	}
	k := outputPosition{index, part}
	if v.slots[k] == nil {
		v.slots[k] = &visibleSlot{}
	}
	return v.slots[k]
}
func (v *visibleConversationOutput) write(i, j int, s string, replace bool) {
	if s == "" {
		return
	}
	slot := v.slot(i, j)
	if replace {
		slot.text.Reset()
	}
	slot.text.WriteString(s)
}
func (v *visibleConversationOutput) position(event, item map[string]any) int {
	id, _ := event["item_id"].(string)
	if id == "" {
		id, _ = item["id"].(string)
	}
	idx := eventInt(event["output_index"], -1)
	if id != "" {
		if v.ids == nil {
			v.ids = map[string]int{}
		}
		if idx < 0 {
			var ok bool
			idx, ok = v.ids[id]
			if !ok {
				idx = len(v.ids)
			}
		}
		v.ids[id] = idx
	}
	return max(idx, 0)
}
func (v *visibleConversationOutput) rememberTool(i int) {
	if v.tools == nil {
		v.tools = map[int]bool{}
	}
	v.tools[i] = true
}
func (v *visibleConversationOutput) item(i int, m map[string]any) {
	typ, _ := m["type"].(string)
	if conversationToolItemTypes[typ] {
		v.rememberTool(i)
		return
	}
	if typ != "message" || (m["role"] != nil && m["role"] != "assistant") {
		return
	}
	if phase, _ := m["phase"].(string); phase == "commentary" || phase == "final_answer" {
		if v.phases == nil {
			v.phases = map[int]string{}
		}
		v.phases[i] = phase
	}
	parts, _ := m["content"].([]any)
	for j, p := range parts {
		part, _ := p.(map[string]any)
		switch part["type"] {
		case "output_text", "text":
			s, _ := part["text"].(string)
			v.write(i, j, s, true)
		case "refusal":
			s, _ := part["refusal"].(string)
			v.write(i, j, s, true)
		}
	}
}
func (v *visibleConversationOutput) observe(event map[string]any) {
	typ, _ := event["type"].(string)
	if typ != "" {
		v.sawEvents = true
	}
	item, _ := event["item"].(map[string]any)
	switch typ {
	case "response.output_item.added", "response.output_item.done":
		v.item(v.position(event, item), item)
	case "response.output_text.delta", "response.refusal.delta", "response.output_text.done", "response.refusal.done":
		done := strings.HasSuffix(typ, ".done")
		key := "delta"
		if done {
			key = "text"
			if typ == "response.refusal.done" {
				key = "refusal"
			}
		}
		s, _ := event[key].(string)
		v.write(v.position(event, nil), eventInt(event["content_index"], 0), s, done)
	}
	body := event
	if resp, ok := event["response"].(map[string]any); ok {
		body = resp
	}
	if out, ok := body["output"].([]any); ok {
		for i, x := range out {
			it, _ := x.(map[string]any)
			v.item(i, it)
		}
	}
	if choices, ok := body["choices"].([]any); ok {
		for i, x := range choices {
			c, _ := x.(map[string]any)
			idx := eventInt(c["index"], i)
			for _, key := range []string{"delta", "message"} {
				m, _ := c[key].(map[string]any)
				v.write(idx, 0, conversationText(m["content"]), key == "message")
				if s, ok := m["refusal"].(string); ok {
					v.write(idx, 1, s, key == "message")
				}
				if calls, ok := m["tool_calls"].([]any); ok {
					for j, raw := range calls {
						call, _ := raw.(map[string]any)
						v.rememberTool(eventInt(call["index"], j))
					}
				}
			}
		}
	}
}
func (v *visibleConversationOutput) parts(outcome string, cut bool) []model.ConversationUserItem {
	keys := make([]outputPosition, 0, len(v.slots))
	for k := range v.slots {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b outputPosition) int {
		if a.item != b.item {
			return cmp.Compare(a.item, b.item)
		}
		return cmp.Compare(a.part, b.part)
	})
	var parts []model.ConversationUserItem
	for _, k := range keys {
		s := v.slots[k].text.String()
		if s == "" {
			continue
		}
		kind := "text"
		switch {
		case outcome != "completed" || cut:
			kind = "partial"
		case v.phases[k.item] == "final_answer":
			kind = "final"
		case v.phases[k.item] == "commentary":
			kind = "commentary"
		}
		parts = append(parts, model.ConversationUserItem{Role: "assistant", Kind: kind, Origin: "response", Text: s, Item: k.item, Part: k.part})
	}
	return parts
}
func visibleText(parts []model.ConversationUserItem) string {
	var ss []string
	for _, p := range parts {
		if p.Origin == "response" && p.Kind != "unknown" {
			ss = append(ss, p.Text)
		}
	}
	return strings.Join(ss, "\n")
}

func recordedConversation(entry *model.ConversationLog, c *model.ConversationContent) (model.ConversationExtraction, int) {
	ex := model.ConversationExtraction{Placeholder: true}
	var req map[string]any
	if common.UnmarshalJsonStr(c.Request, &req) == nil && req != nil && req["capture"] == nil {
		ex = extractConversationRequest(req)
	}
	if ex.PrevResponseId == "" {
		ex.PrevResponseId = entry.PreviousResponseId
	}
	var value any
	_ = common.UnmarshalJsonStr(c.Response, &value)
	v := visibleConversationOutput{}
	switch data := value.(type) {
	case map[string]any:
		v.observe(data)
	case []any:
		for _, x := range data {
			if ev, ok := x.(map[string]any); ok {
				v.observe(ev)
			}
		}
	}
	ex.Outputs = v.parts(entry.Outcome, entry.Truncated)
	if c.ClientReference != "" {
		ex.ClientKey = c.ClientReference
	}
	// Only the recorder sets spans; they refer to actually written, visible bytes,
	// independent of whether the legacy terminal JSON includes the delta events.
	if len(c.OutputSpans) > 0 {
		offset := 0
		var stored []model.ConversationUserItem
		for i, span := range c.OutputSpans {
			if i > 0 {
				if offset >= len(c.Output) || c.Output[offset] != '\n' {
					break
				}
				offset++
			}
			if span.Bytes <= 0 || span.Bytes > len(c.Output)-offset {
				break
			}
			switch span.Kind {
			case "text", "commentary", "final", "partial":
			default:
				return ex, len(v.tools)
			}
			stored = append(stored, model.ConversationUserItem{Role: "assistant", Kind: span.Kind, Origin: "response", Item: span.Item, Part: span.Part, Text: c.Output[offset : offset+span.Bytes]})
			offset += span.Bytes
		}
		if len(stored) == len(c.OutputSpans) && offset == len(c.Output) {
			ex.Outputs = stored
		}
	}
	if len(ex.Outputs) == 0 && c.Output != "" && !v.sawEvents {
		// Legacy terminal summaries sometimes discarded all delta events. Preserve
		// the old text as unverified evidence; never certify it as an assistant answer.
		ex.Outputs = []model.ConversationUserItem{{Role: "assistant", Kind: "unknown", Origin: "legacy", Text: c.Output}}
	}
	return ex, len(v.tools)
}

// ApplyConversationReading derives a root-only reading view without altering
// the recorded request, response or compressed archive. No body goes to logs.
func ApplyConversationReading(entry *model.ConversationLog, c *model.ConversationContent) error {
	ex, _ := recordedConversation(entry, c)
	c.ClientReference = ""
	c.OutputSpans = nil
	reading := &model.ConversationReading{Input: ex.UserItems, Replies: ex.Outputs}
	var focused []string
	if entry.SessionState == "linked" || entry.SessionState == "unassigned" {
		var rows []model.ConversationMessage
		if err := model.DB.Where("request_id = ?", entry.RequestId).Order("id").Find(&rows).Error; err != nil {
			return err
		}
		reading.Replies = nil
		for i := range reading.Input {
			if reading.Input[i].Origin == "request" {
				reading.Input[i].Origin = "snapshot"
			}
		}
		for _, m := range rows {
			part := model.ConversationUserItem{Role: m.Role, Kind: m.Kind, Origin: m.Origin, Text: m.Content, Item: m.Item, Part: m.Part, Truncated: m.Truncated}
			if m.Role == "assistant" && m.Origin != "snapshot" {
				reading.Replies = append(reading.Replies, part)
				continue
			}
			if (m.Origin == "legacy" && m.Role == "user") || (m.Origin == "snapshot" && len(ex.UserItems) == 0) {
				reading.Input = append(reading.Input, part)
				continue
			}
			if m.Origin != "request" {
				continue
			}
			matched := false
			for i := range reading.Input {
				p := &reading.Input[i]
				if p.Role == part.Role && p.Item == part.Item && p.Part == part.Part && p.Kind == part.Kind && p.Hash == m.ContentHash {
					p.Origin = "request"
					matched = true
					break
				}
			}
			if !matched {
				reading.Input = append(reading.Input, part)
			}
		}
	}
	for _, p := range reading.Input {
		if p.IsQuestion() && p.Origin == "request" {
			focused = append(focused, p.Text)
		}
	}
	c.LatestInput = strings.Join(focused, "\n")
	c.Output = visibleText(reading.Replies)
	c.Reading = reading
	return nil
}
