package model

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestConversationInputAttribution(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []string
		kinds       []string
	}{
		{"IDE wrapper", "# Context from my IDE setup:\nFile: main.go\n## My request for Codex:\nReview this", []string{"Review this"}, []string{"context", "text"}},
		{"quoted heading", "Please explain ## My request: here", []string{"Please explain ## My request: here"}, []string{"text"}},
		{"fenced example", "```md\n## My request:\nquoted\n```", []string{"```md\n## My request:\nquoted\n```"}, []string{"text"}},
		{"quoted marker in file appendix", "# Files mentioned by the user:\n```md\n## My request:\nexample\n```", nil, []string{"unknown"}},
		{"pure environment", "<environment_context>fixture</environment_context>", nil, []string{"context"}},
		{"mixed XML", "<environment_context>fixture</environment_context>\nActual question", []string{"Actual question"}, []string{"context", "text"}},
		{"project instructions", "# AGENTS.md instructions for demo\n<INSTRUCTIONS>fixture</INSTRUCTIONS>", nil, []string{"context"}},
		{"long ordinary draft", strings.Repeat("字", 1600), []string{strings.Repeat("字", 1600)}, []string{"text"}},
		{"unrecognised wrapper", "<custom_context>fixture</custom_context>", nil, []string{"unknown"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var qs, ks []string
			for _, p := range ConversationQAParts(tc.input) {
				ks = append(ks, p.Kind)
				if p.Kind == "text" {
					qs = append(qs, strings.TrimSpace(p.Text))
				}
			}
			require.Equal(t, tc.want, qs)
			require.Equal(t, tc.kinds, ks)
		})
	}
}
