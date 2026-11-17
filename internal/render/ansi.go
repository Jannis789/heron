package render

import "regexp"

// ansiRe entfernt ANSI-SGR-Sequenzen (fuer Yank/Markdown, FR-29).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI entfernt ANSI-Style-Escapes aus s — Kopie fuer Zwischenablage /
// Markdown (sauberes Yank, FR-29).
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// ansiSeqLen liefert die Laenge der Escape-Sequenz ab Index i (das auf 0x1b
// zeigen muss): CSI (ESC [ Parameter/Intermediate + finales Byte), OSC
// (ESC ] ... BEL|ST) oder 2-Zeichen-Escape. Ohne Abschluss laeuft sie bis
// zum Stringende — der Rest gehoert dann ohnehin in dieselbe Ausgabezeile.
// wrap/substring-Code DARF Sequenzen nur GANZ konsumieren: ein halber SGR
// erscheint im Terminal als sichtbarer Muell ("[1msonstiges:[22m").
func ansiSeqLen(rs []rune, i int) int {
	n := len(rs)
	if i >= n {
		return 0
	}
	if i+1 >= n {
		return n - i
	}
	switch rs[i+1] {
	case '[':
		j := i + 2
		for j < n && ((rs[j] >= 0x30 && rs[j] <= 0x3f) || (rs[j] >= 0x20 && rs[j] <= 0x2f)) {
			j++
		}
		if j < n {
			j++ // finales Byte
		}
		return j - i
	case ']':
		j := i + 2
		for j < n && rs[j] != 0x07 && !(rs[j] == 0x1b && j+1 < n && rs[j+1] == 0x5c) {
			j++
		}
		if j < n {
			if rs[j] == 0x07 {
				j++
			} else {
				j += 2 // ST = ESC \
			}
		}
		return j - i
	}
	return 2 // 2-Zeichen-Escape (ESC + finales Byte)
}
