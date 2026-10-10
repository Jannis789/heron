// Package clipboard writes to the terminal clipboard: OSC52 is the primary,
// SSH-safe path; native tools (wl-copy, xclip, pbcopy) are the fallback for
// terminals that ignore OSC52 (FR-5).
package clipboard

import (
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Fallback is a native clipboard command that reads text from stdin.
type Fallback struct {
	Name string
	Args []string
}

// NativeFallbacks lists the fallback commands in priority order (FR-5).
func NativeFallbacks() []Fallback {
	return []Fallback{
		{Name: "wl-copy"},
		{Name: "xclip", Args: []string{"-selection", "clipboard"}},
		{Name: "pbcopy"},
	}
}

// osc52Frame returns the OSC52 sequence for text (works over SSH).
func osc52Frame(text string) string {
	payload := base64.StdEncoding.EncodeToString([]byte(text))
	return "\x1b]52;c;" + payload + "\x07"
}

// WriteOSC52 writes the OSC52 sequence for text to w.
func WriteOSC52(w io.Writer, text string) error {
	_, err := io.WriteString(w, osc52Frame(text))
	return err
}

// Copy writes text to the terminal clipboard via OSC52 and, in parallel, via
// the first available native tool. OSC52 is primary (SSH-tauglich, kein
// subprocess nötig); die native Kette deckt Terminals ab, die OSC52
// ignorieren. The native run happens in a goroutine so the UI never waits.
func Copy(text string) {
	_ = WriteOSC52(os.Stdout, text)
	go RunFallback(text)
}

// pickFallback returns the first fallback whose program exists, together with
// its resolved path. The path must be used for execution — resolving it and
// then running the bare name would fall back to a fresh PATH lookup.
func pickFallback(fs []Fallback, lookPath func(string) (string, error)) (Fallback, string, bool) {
	for _, f := range fs {
		if p, err := lookPath(f.Name); err == nil && p != "" {
			return f, p, true
		}
	}
	return Fallback{}, "", false
}

// RunFallback pushes text through the first available native clipboard tool
// and reports which tool ran ("" when none is installed or the run failed).
// It is safe to call from a goroutine.
func RunFallback(text string) string {
	return runWith(NativeFallbacks(), exec.LookPath, text)
}

// runWith resolves the first available fallback via lookPath and feeds text
// to it on stdin. lookPath is injected so tests can run a stub tool without
// touching the real clipboard or the process PATH.
func runWith(fs []Fallback, lookPath func(string) (string, error), text string) string {
	f, path, ok := pickFallback(fs, lookPath)
	if !ok {
		return ""
	}
	cmd := exec.Command(path, f.Args...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return ""
	}
	return f.Name
}
