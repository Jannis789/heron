package render

import "regexp"

// ansiRe entfernt ANSI-SGR-Sequenzen (fuer Yank/Markdown, FR-29).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI entfernt ANSI-Style-Escapes aus s — Kopie fuer Zwischenablage /
// Markdown (sauberes Yank, FR-29).
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }
