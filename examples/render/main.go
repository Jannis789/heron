// Command render prints a heron block render of a markdown file. When stdout
// is a terminal it watches SIGWINCH and re-renders on resize, so the dynamic
// column layout can be evaluated live (Ctrl+C to quit).
package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"heron/internal/render"
	"heron/internal/tui"
)

// detectWidth prefers the real terminal size (stdout, then stdin) over a
// possibly stale COLUMNS variable.
func detectWidth(fallback int) int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	if w, _, err := term.GetSize(int(os.Stdin.Fd())); err == nil && w > 0 {
		return w
	}
	if c := os.Getenv("COLUMNS"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func renderFile(path string, width int, th *render.Theme) (string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	nodes, err := render.Parse(string(src))
	if err != nil {
		return "", err
	}
	widgets := tui.BuildWidgets(nodes, th, nil)
	var b strings.Builder
	for i, w := range widgets {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(w.View(width))
		b.WriteString("\n")
	}
	return b.String(), nil
}

// emit prints the render with the theme background filling every line.
func emit(s string, width int, th *render.Theme) {
	seq := th.BackgroundSeq()
	for _, ln := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if pad := width - lipgloss.Width(ln); pad > 0 {
			ln += strings.Repeat(" ", pad)
		}
		if seq != "" {
			ln = seq + strings.ReplaceAll(ln, "\x1b[0m", "\x1b[0m"+seq)
		}
		fmt.Println(ln)
	}
}

func main() {
	// Manual arg parsing so the flag works in any position (flag.Parse stops
	// at the first positional argument).
	var width int
	var paths []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-width", "--width":
			if i+1 < len(args) {
				i++
				if n, err := strconv.Atoi(args[i]); err == nil {
					width = n
				}
			}
		default:
			paths = append(paths, args[i])
		}
	}
	path := "examples/reply.md"
	if len(paths) > 0 {
		path = paths[0]
	}
	th, err := render.DefaultTheme()
	if err != nil {
		fmt.Fprintf(os.Stderr, "theme: %v\n", err)
		os.Exit(1)
	}

	if width > 0 {
		out, err := renderFile(path, width, th)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		emit(out, width, th)
		return
	}

	if !term.IsTerminal(int(os.Stdout.Fd())) {
		// Piped: one-shot render at the detected width.
		w := detectWidth(80)
		out, err := renderFile(path, w, th)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		emit(out, w, th)
		return
	}

	// Interactive: re-render on terminal resize so the dynamic layout is
	// visible live (like pi renders its tables).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	for {
		w := detectWidth(80)
		fmt.Print("\x1b[H\x1b[2J")
		fmt.Printf("width %d · resize to reflow · Ctrl+C quits\n\n", w)
		out, err := renderFile(path, w, th)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		emit(out, w, th)
		<-sig
	}
}
