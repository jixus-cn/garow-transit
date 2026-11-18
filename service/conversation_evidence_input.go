package service

import (
	"github.com/QuantumNous/new-api/model"
	"strings"
	"unicode/utf8"
)

const qaTextLimit = 1 << 20

type qaSpan struct {
	Message int    `json:"message"`
	Block   int    `json:"block"`
	Start   int    `json:"start"`
	Bytes   int    `json:"bytes"`
	State   string `json:"state,omitempty"`
}
type qaInput struct {
	text              strings.Builder
	spans             []qaSpan
	partial, redacted bool
}

func (p *qaInput) gap(i, j int, reason string) {
	p.partial = true
	if len(p.spans) < 2048 {
		p.spans = append(p.spans, qaSpan{Message: i, Block: j, Start: p.text.Len(), State: reason})
	}
}
func (p *qaInput) add(s string, i, j int) {
	if s == "" {
		return
	}
	if len(p.spans) >= 2048 {
		p.partial = true
		return
	}
	s, masked := model.MaskQACredentials(s)
	state := ""
	if masked {
		state = "redacted"
		p.redacted = true
	}
	left := qaTextLimit - p.text.Len()
	if p.text.Len() > 0 {
		left--
	}
	if left <= 0 {
		p.gap(i, j, "too_large")
		return
	}
	if len(s) > left {
		for left > 0 && !utf8.RuneStart(s[left]) {
			left--
		}
		s = s[:left]
		p.partial = true
		state = "truncated"
	}
	if p.text.Len() > 0 {
		p.text.WriteByte('\n')
	}
	off := p.text.Len()
	p.text.WriteString(s)
	p.spans = append(p.spans, qaSpan{Message: i, Block: j, Start: off, Bytes: len(s), State: state})
}
