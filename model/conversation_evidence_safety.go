package model

import (
	"regexp"
	"strings"
)

var qaPEM = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|\z)`)
var qaAuthHeader = regexp.MustCompile(`(?im)(\b(?:authorization|proxy-authorization|set-cookie|cookie)[ \t]*:[ \t]*)[^ \t\r\n][^\r\n]*`)
var qaAssignedSecret = regexp.MustCompile(`(?i)(["']?(?:api[-_]?key|access[-_]?token|refresh[-_]?token|session[-_]?(?:token|cookie)|password|passwd|client[-_]?secret)["']?\s*[:=]\s*)(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;}\]]+)`)
var qaKey = regexp.MustCompile(`(?:\bsk-[A-Za-z0-9_-]{12,}|\bgh[pousr]_[A-Za-z0-9_]{20,}|\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)`)

func MaskQACredentials(s string) (string, bool) {
	t := qaPEM.ReplaceAllString(s, "[redacted]")
	t = qaAuthHeader.ReplaceAllString(t, "${1}[redacted]")
	t = qaAssignedSecret.ReplaceAllString(t, `${1}"[redacted]"`)
	t = qaKey.ReplaceAllString(t, "[redacted]")
	t = strings.ReplaceAll(t, "\x00", "\uFFFD")
	return t, t != s
}
