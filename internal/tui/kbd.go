package tui

import (
	"io"
	"os"
	"strconv"
	"strings"
)

// ---- Shift+Enter im Kitty-Tastaturprotokoll (ghostty/kitty) -----------------
//
// bubbletea v1 kann shift+enter nicht von enter unterscheiden: Die meisten
// Terminals senden fuer beide schlicht \r. ghostty und koennen mehr — das
// Kitty-Tastaturprotokoll (Flag 1, "disambiguate escape codes") liefert
// shift+enter als EIGENE Sequenz \x1b[13;2u. bubbletea v1.3 parst sie
// nicht: Der Parser zerschneidet sie in alt+"[" plus Runen-Muell ("13;2u"
// landet als Getippe im Eingabefeld — dieselbe Klasse wie die Kitty-Grafik-
// Antworten im kittyResponseFilter, FR-27).
//
// Loesung: Ein uebersetzender Reader VOR dem Programm (tea.WithInput) plus
// Push/Pop des Protokolls auf dem Terminal:
//
//	\x1b[13;2u  shift+enter -> \x1b[Z  (shift+tab — die App behandelt
//	                                   beide Trigger identisch)
//	\x1b[27u    esc         -> \x1b    (disambiguate veraendert die
//	                                   Esc-Taste selbst!)
//	weitere CSI-u-Tasten    -> verworfen (ctrl+enter etc. sind ungebunden,
//	                                   Runen-Muell waere schlimmer)
//
// Die Felder dekodiert kittyKeyCSIu liberal (WezTerm meldet Modifier
// IMMER, dazu Event-Typ und Text: "27;1u", "13;2;13u", Releases ":3"
// sind keine exakten Strings mehr).
//
// Ausserhalb von ghostty/kitty bleibt alles beim Alten: shift+tab ist der
// zuverlaessige Ausloeser. HERON_KBD=off schaltet das Protokoll ab,
// HERON_KBD=force erzwingt es (Diagnose).

// kittyKbdEnabled entscheidet, ob das Kitty-Protokoll aktiviert wird:
// an JEDEM echten TTY (das Protokoll reist auch durch SSH zum lokalen
// Terminal — TERM sagt dort nur xterm-256color, die Sequenzen kommen
// trotzdem zurueck). Nur Multiplexer (tmux/screen) zerschneiden die
// CSI-u-Eingaben — dort bleibt es aus. HERON_KBD=off schaltet ab,
// =force erzwingt (Diagnose).
func kittyKbdEnabled(stdin *os.File) bool {
	switch os.Getenv("HERON_KBD") {
	case "off":
		return false
	case "force":
		return true
	}
	if stdin == nil {
		return false
	}
	if fi, err := stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false // gepipt/umgeleitet: kein Terminal, kein Protokoll
	}
	term := os.Getenv("TERM")
	if strings.Contains(term, "tmux") || strings.Contains(term, "screen") {
		return false // Multiplexer zerschneiden CSI-u — lieber legacy
	}
	return true
}

// kittyReader uebersetzt CSI-u-Tasten des Kitty-Protokolls in Sequenzen,
// die bubbletea v1 versteht. Er implementiert Fd()/Write()/Close(), damit
// bubbletea Raw-Mode auf dem echten stdin-Fd setzt (charmbracelet/x/term
// File-Interface) — ein plain io.Reader wuerde ohne Raw-Mode zeilenweise
// lesen und die Tastatur komplett brechen.
type kittyReader struct {
	src     io.Reader
	file    *os.File // das gewrappte *os.File (Fd/Write/Close), nil in Tests
	restore func()   // Pop des Protokolls (Shutdown)

	// carry haelt eine UNVOLLSTAENDIGE Sequenz vom letzten Read (split
	// reads: \x1b[13; erst im naechsten Chunk vollstaendig); pending ist
	// fertige Ausgabe, die nicht mehr in den Aufrufer passte.
	carry   []byte
	pending []byte
}

// newKittyKeyboard schaltet das Kitty-Protokoll auf dem Terminal an (Push)
// und liefert den uebersetzenden Reader fuer stdin — oder nil, wenn das
// Terminal es nicht unterstuetzt. Restore() poppt den Stack wieder.
func newKittyKeyboard(stdin *os.File, stdout io.Writer) *kittyReader {
	if !kittyKbdEnabled(stdin) {
		return nil
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	kr := &kittyReader{src: stdin, file: stdin}
	_, _ = stdout.Write([]byte("\x1b[>1u"))                        // Push: Flag 1 (disambiguate)
	kr.restore = func() { _, _ = stdout.Write([]byte("\x1b[<u")) } // Pop
	return kr
}

// wrapKittyInput verpackt einen beliebigen Reader in die uebersetzende
// Sicht (Tests: io.Pipe) — ohne Terminal-Push/Pop.
func wrapKittyInput(r io.Reader) io.Reader {
	kr := &kittyReader{src: r}
	if f, ok := r.(*os.File); ok {
		kr.file = f
	}
	return kr
}

func (k *kittyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(k.pending) == 0 {
		var buf [512]byte
		n, err := k.src.Read(buf[:])
		if n > 0 {
			chunk := buf[:n]
			if len(k.carry) > 0 {
				chunk = append(k.carry, chunk...)
				k.carry = nil
			}
			out, held := translateKitty(chunk)
			k.pending = append(k.pending, out...)
			k.carry = held
		}
		if n == 0 && err != nil {
			return 0, err
		}
	}
	n := copy(p, k.pending)
	k.pending = k.pending[n:]
	return n, nil
}

// Restore poppt das Protokoll vom Terminal-Stack (idempotent).
func (k *kittyReader) Restore() {
	if k.restore != nil {
		k.restore()
		k.restore = nil
	}
}

// Fd/Write/Close delegieren an das gewrappte File — das erfuellt
// charmbracelet/x/term.File, bubbletea setzt damit Raw-Mode auf stdin.
func (k *kittyReader) Fd() uintptr {
	if k.file == nil {
		return 0
	}
	return k.file.Fd()
}

func (k *kittyReader) Write(b []byte) (int, error) {
	if k.file == nil {
		return len(b), nil // Tests: Reader-Only
	}
	return k.file.Write(b)
}

func (k *kittyReader) Close() error {
	if k.file == nil {
		return nil
	}
	return k.file.Close()
}

// translateKitty faellt CSI-u-Sequenzen (Kitty-Tastaturprotokoll) auf
// bubbletea-verstaendliche Bytes zurueck. Zurueck kommen die Ausgabe und
// der HALT: eine unvollstaendige Sequenz am Pufferende, die erst mit dem
// naechsten Read vervollstaendigt wird. Legacy-Sequenzen (Pfeile, Maus,
// F-Tasten, Bracketed Paste) bleiben unangetastet — das Protokoll-Fragment
// "disambiguate" veraendert nur Esc und MODIFIZIERTE Texttasten.
func translateKitty(b []byte) (out, held []byte) {
	out = make([]byte, 0, len(b))
	i := 0
	for i < len(b) {
		if b[i] != 0x1b {
			out = append(out, b[i])
			i++
			continue
		}
		// Bloedes ESC am Pufferende: SOFORT durchreichen. Mit AKTIVEM
		// Protokoll kommt die Esc-Taste als \x1b[27u (oben uebersetzt) —
		// ein nacktes \x1b ist dort nur ein Split-Read-Rest. Ohne Protokoll
		// (Terminal ignoriert den Push — z.B. aeltere ghostty: shift+enter
		// kam als \r) ist das nackte \x1b ABER die ESC-TASTE: HIER HALTEN
		// wuerde sie schlucken, bis die naechste Taste kommt — und dann als
		// alt+X verschmelzen. Der Modus waere eine Falle ohne Ausgang.
		// bubbletea parst ein solitaeres \x1b ohnehin als KeyEsc — genau
		// wie ohne diesen Reader auch.
		if i+1 >= len(b) {
			out = append(out, b[i:]...)
			return out, nil
		}
		if b[i+1] != '[' {
			// Alt+Taste & Co: ESC gefolgt von einem Zeichen — durchreichen.
			out = append(out, b[i:i+2]...)
			i += 2
			continue
		}
		// CSI: Parameter scannen (Ziffern, ;, :, ?) bis zum Finalbyte.
		// Auch die privaten Introducer < = > gehoeren dazu: SGR-Maus-Events
		// sind CSI-<-Sequenzen (\x1b[<64;97;43M Mausrad, Regression
		// 2026-11-01) — ohne sie hier stoppte der Scan am '<', gab NUR
		// "\x1b[<" als Legacy weiter und der Rest ("64;97;43M") lief als
		// Runen-Muell ins Eingabefeld. Gleiches gilt fuer den Kitty-Pop
		// \x1b[<u am Exit.
		j := i + 2
		for j < len(b) && (b[j] >= '0' && b[j] <= '9' || b[j] == ';' || b[j] == ':' || b[j] == '?' ||
			b[j] == '<' || b[j] == '=' || b[j] == '>') {
			j++
		}
		if j == len(b) {
			held = append(held, b[i:]...) // Sequenz unvollstaendig
			return out, held
		}
		// CSI-u gilt nur OHNE privaten Introducer: \x1b[27u ist die Esc-Taste,
		// \x1b[<u dagegen der Kitty-Protokoll-POP (und Maussequenzen enden
		// ohnehin auf M/m) — die muessten unangetastet durch.
		if b[j] == 'u' && b[i+2] != '<' && b[i+2] != '=' && b[i+2] != '>' {
			if seq, ok := kittyKeyCSIu(string(b[i+2 : j])); ok {
				out = append(out, seq...)
			}
			i = j + 1
			continue
		}
		// Legacy-Sequenz: unveraendert durchreichen.
		out = append(out, b[i:j+1]...)
		i = j + 1
	}
	return out, held
}

// Kitty-Protokoll: Modifier-Bitmaske = Feldwert - 1 (kitty-Spec).
const (
	kittyShift = 1 << iota // 0x01
	kittyAlt               // 0x02
	kittyCtrl              // 0x04
	kittySuper             // 0x08
	kittyHyper             // 0x10
	kittyMeta              // 0x20
	// Lock-Bits (Caps 0x40, Num 0x80) sind kein echter Modifier — sie
	// duerfen eine Taste nicht veraendern (WezTerm meldet sie MIT).
)

// kittyKeyCSIu uebersetzt die Parameter einer CSI-u-Taste (Kitty-Tastatur-
// protokoll) in eine bubbletea-verstaendliche Sequenz; ok=false verwirft
// die Taste. Das Feldformat ist (kitty-Spec):
//
//	CSI key:alternate ; modifiers:event ; text u
//
// Jedes Terminal fuellt es ANDERS aus — kitty/ghostty senden nur die
// noetigen Felder, WezTerm haengt Modifier IMMER an, gern auch Event-Typ
// und zugehoerigen Text (User-Report "ich kann nicht aus dem Cursor-Mode
// gehen": Der fruehere exakte String-Vergleich "13;2"/"27" warf die
// WezTerm-Formen STUMM WEG — esc versackte, der Modus hatte keinen Ausgang):
//
//	\x1b[27u        kitty/ghostty: Esc
//	\x1b[27;1u      WezTerm: Esc ohne Modifier (Feld steht immer da)
//	\x1b[27;1:1u    WezTerm: Esc, Event-Typ press
//	\x1b[27;129:3u  WezTerm: Esc mit NumLock, RELEASE
//	\x1b[13;2u      shift+enter
//	\x1b[13;2:2u    shift+enter, repeat
//	\x1b[13;2;13u   WezTerm: shift+enter MIT Text-Codepoint
func kittyKeyCSIu(params string) ([]byte, bool) {
	num := func(s string) int {
		if i := strings.IndexByte(s, ':'); i >= 0 {
			s = s[:i] // Sub-Parameter (alternate keys, event types) ignorieren
		}
		n, _ := strconv.Atoi(s)
		return n
	}
	fields := strings.Split(params, ";")
	mods, event := 1, 1 // Default der Spec: keine Modifier, press
	if len(fields) > 1 {
		mods = num(fields[1])
		sub := strings.Split(fields[1], ":")
		if len(sub) > 1 {
			event = num(sub[1])
		}
	}
	if event == 3 {
		return nil, false // Release: NICHT als zweiten Druck melden
	}
	mask := mods - 1 // Feld = Bitmaske + 1
	if mask < 0 {
		mask = 0
	}
	const foreign = kittyAlt | kittyCtrl | kittySuper | kittyHyper | kittyMeta
	switch key := num(fields[0]); key {
	case 27: // Esc — Locks und Shift veraendern sie nicht
		if mask&foreign == 0 {
			return []byte{0x1b}, true
		}
	case 13: // Enter: nur MIT shift ist es der Cursor-Trigger
		if mask&foreign == 0 && mask&kittyShift != 0 {
			return []byte{0x1b, '[', 'Z'}, true // shift+enter -> shift+tab
		}
	}
	return nil, false // ungebunden: verwerfen statt Runen-Muell
}
