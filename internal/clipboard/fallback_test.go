package clipboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteOSC52MatchesFrame pins the primary (SSH-safe) output format.
func TestWriteOSC52MatchesFrame(t *testing.T) {
	var b strings.Builder
	if err := WriteOSC52(&b, "hallo"); err != nil {
		t.Fatalf("write: %v", err)
	}
	want := osc52Frame("hallo")
	if b.String() != want {
		t.Fatalf("got %q want %q", b.String(), want)
	}
	if !strings.HasPrefix(b.String(), "\x1b]52;c;") {
		t.Fatalf("not an OSC52 sequence: %q", b.String())
	}
}

// TestPickFallbackPriority asserts the documented chain wl-copy > xclip > pbcopy.
func TestPickFallbackPriority(t *testing.T) {
	present := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, a := range names {
				if a == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", os.ErrNotExist
		}
	}

	name := func(f Fallback, _ string, _ bool) string { return f.Name }

	if got := name(pickFallback(NativeFallbacks(), present("wl-copy", "xclip", "pbcopy"))); got != "wl-copy" {
		t.Fatalf("want wl-copy first, got %q", got)
	}
	if got := name(pickFallback(NativeFallbacks(), present("xclip", "pbcopy"))); got != "xclip" {
		t.Fatalf("want xclip after wl-copy miss, got %q", got)
	}
	if got := name(pickFallback(NativeFallbacks(), present("pbcopy"))); got != "pbcopy" {
		t.Fatalf("want pbcopy as last resort, got %q", got)
	}
	if _, _, ok := pickFallback(NativeFallbacks(), present()); ok {
		t.Fatal("expected no fallback when nothing is installed")
	}

	// The resolved path must be returned, not just the command name.
	f, p, ok := pickFallback(NativeFallbacks(), present("wl-copy"))
	if !ok || p != "/usr/bin/wl-copy" || f.Name != "wl-copy" {
		t.Fatalf("resolved path lost: name=%q path=%q ok=%v", f.Name, p, ok)
	}
}

// fakeTool writes an executable stub that stores its stdin in a file and
// returns (stub path, capture file).
func fakeTool(t *testing.T, name string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "clip.txt")
	script := "#!/bin/sh\ncat > '" + out + "'\n"
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return p, out
}

// only makes lookPath resolve exactly one stub by absolute path — no PATH
// manipulation, so the real clipboard can never be touched.
func only(name, path string) func(string) (string, error) {
	return func(n string) (string, error) {
		if n == name {
			return path, nil
		}
		return "", os.ErrNotExist
	}
}

// TestRunFallbackUsesFirstAvailable runs the fallback chain against a real
// stub process and proves the text arrives via stdin.
func TestRunFallbackUsesFirstAvailable(t *testing.T) {
	path, out := fakeTool(t, "wl-copy")

	if got := runWith(NativeFallbacks(), only("wl-copy", path), "zugestellt"); got != "wl-copy" {
		t.Fatalf("runWith=%q want wl-copy", got)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read stub output: %v", err)
	}
	if string(b) != "zugestellt" {
		t.Fatalf("tool got %q want %q", b, "zugestellt")
	}
}

// TestRunFallbackSkipsMissingTools proves the chain falls through to xclip
// when wl-copy is not installed.
func TestRunFallbackSkipsMissingTools(t *testing.T) {
	path, out := fakeTool(t, "xclip")

	if got := runWith(NativeFallbacks(), only("xclip", path), "via-xclip"); got != "xclip" {
		t.Fatalf("runWith=%q want xclip", got)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read stub output: %v", err)
	}
	if string(b) != "via-xclip" {
		t.Fatalf("tool got %q", b)
	}
}

// TestRunFallbackNoneAvailable: nothing installed -> "" and no panic.
func TestRunFallbackNoneAvailable(t *testing.T) {
	none := func(string) (string, error) { return "", os.ErrNotExist }
	if got := runWith(NativeFallbacks(), none, "x"); got != "" {
		t.Fatalf("runWith=%q want empty", got)
	}
}

// TestRunFallbackFailureIsSilent: a failing tool reports "" (fallback is
// best-effort, never an error the UI has to handle).
func TestRunFallbackFailureIsSilent(t *testing.T) {
	// A stub that exits non-zero.
	dir := t.TempDir()
	p := filepath.Join(dir, "wl-copy")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	if got := runWith(NativeFallbacks(), only("wl-copy", p), "x"); got != "" {
		t.Fatalf("runWith=%q want empty on failure", got)
	}
}
