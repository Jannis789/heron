package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// plantumlTimeout bounds one conversion batch so a wedged converter process
// cannot leave a block pending forever (NFR-7).
const plantumlTimeout = 20 * time.Second

// diagramJob is one pending conversion: the stable block id, the fence
// language (plantuml | mermaid) and the raw source that stays visible until
// the result arrives.
type diagramJob struct {
	id   int
	lang string
	code string
}

// diagramResult is the outcome for a single block. ok=false means the
// conversion failed — the raw source then remains (FR-6/FR-28 fallback).
// png != nil carries an image for the Kitty protocol (FR-27); ascii holds
// the text fallback that Copy/Markdown keep serving.
type diagramResult struct {
	id    int
	lang  string
	ascii string
	png   []byte
	ok    bool
}

// diagramsDoneMsg carries every result of one conversion batch, keyed by the
// stable block id.
type diagramsDoneMsg struct {
	results []diagramResult
}

// diagramJobs lists the blocks that still need conversion (plantuml and
// mermaid fences, FR-6/FR-28).
func (m Model) diagramJobs() []diagramJob {
	var jobs []diagramJob
	for _, it := range m.chat.items {
		cb, ok := it.(CodeBlock)
		if !ok || (cb.lang != "plantuml" && cb.lang != "mermaid") || cb.converted || cb.id < 1 {
			continue
		}
		jobs = append(jobs, diagramJob{id: cb.id, lang: cb.lang, code: cb.code})
	}
	return jobs
}

// pendingDiagrams starts the conversion for all pending blocks (plantuml to
// ASCII or PNG, mermaid to PNG when mermaid-cli is available). It runs as a
// tea.Cmd (goroutine), so the event loop is never blocked (NFR-2); the block
// keeps showing the raw source until diagramsDoneMsg arrives.
func (m Model) pendingDiagrams() tea.Cmd {
	jobs := m.diagramJobs()
	if len(jobs) == 0 {
		return nil
	}
	return func() tea.Msg {
		imageLogf("convert start: %d job(s), imagesEnabled=%v", len(jobs), m.imagesEnabled())
		ctx, cancel := context.WithTimeout(context.Background(), plantumlTimeout)
		defer cancel()
		results := make([]diagramResult, 0, len(jobs))
		for _, j := range jobs {
			res := diagramResult{id: j.id, lang: j.lang, ascii: j.code}
			t0 := time.Now()
			switch j.lang {
			case "mermaid":
				// FR-28: PNG nur im experimentellen force-Modus mit
				// installiertem mmdc und bildfaehigem Terminal; ohne
				// bleibt der Klartext.
				if m.imagesEnabled() {
					if png, err := render.MermaidToPNG(ctx, j.code); err == nil {
						res.png = png
						res.ok = true
					}
				}
			default:
				// FR-6: Unicode-ASCII ist der Default-Pfad. PNG nur im
				// experimentellen force-Modus mit bildfaehigem Terminal
				// (FR-27, s. ImageBlock).
				if m.imagesEnabled() {
					if png, err := render.PlantUMLToPNG(ctx, j.code); err == nil {
						res.png = png
						res.ok = true
					}
				}
				if res.png == nil {
					if ascii, err := render.PlantUMLToASCII(ctx, j.code); err == nil {
						res.ascii = ascii
						res.ok = true
					}
				}
			}
			if res.png != nil {
				imageLogf("convert %s id=%d ok png=%d bytes (%.1fms)", j.lang, j.id, len(res.png), float64(time.Since(t0).Microseconds())/1000.0)
			} else if res.ascii != "" && res.ascii != j.code {
				imageLogf("convert %s id=%d ok ascii (%d bytes, %.1fms)", j.lang, j.id, len(res.ascii), float64(time.Since(t0).Microseconds())/1000.0)
			} else {
				imageLogf("convert %s id=%d FAILED ascii (%.1fms) -> roh-Fallback", j.lang, j.id, float64(time.Since(t0).Microseconds())/1000.0)
			}
			results = append(results, res)
		}
		return diagramsDoneMsg{results: results}
	}
}

// imageDebugOn kontrolliert das Diagnose-Log (/tmp/heron-images.log) via
// HERON_IMAGE_DEBUG=1 (FR-27). Ohne das Flag ist das Debugging vollstaendig
// aus — der Produktivpfad hat keinen Overhead.
func imageDebugOn() bool {
	return strings.ToLower(os.Getenv("HERON_IMAGE_DEBUG")) == "1" ||
		strings.ToLower(os.Getenv("HERON_IMAGE_DEBUG")) == "true"
}

// imageLog schreibt eine Zeile ins Diagnose-Log. Nur aktiv mit
// HERON_IMAGE_DEBUG=1 (s. imageDebugOn). Threadsafe ueber den Mutex.
var imageLogMu sync.Mutex

func imageLogf(format string, a ...interface{}) {
	if !imageDebugOn() {
		return
	}
	imageLogMu.Lock()
	defer imageLogMu.Unlock()
	f, err := os.OpenFile("/tmp/heron-images.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s "+format+"\n", append([]interface{}{time.Now().Format("15:04:05.000")}, a...)...)
}

// applyDiagrams installs the conversion results in the chat: PNG results
// become ImageBlocks (FR-27), ASCII results replace the source, failures
// keep the raw source. Either way the block is marked converted so it is
// never queued again.
func (l BlockList) applyDiagrams(results []diagramResult) (BlockList, int) {
	applied := 0
	for _, r := range results {
		for i, it := range l.items {
			cb, ok := it.(CodeBlock)
			if !ok || cb.id != r.id {
				continue
			}
			if r.png != nil {
				// Halbblock-Bild ist reiner Text und damit immer renderbar
				// (FR-27). Der Block wird scroll-freundlich auf die chat-
				// Hoehe begrenzt, damit sehr grosse Diagramme nicht den
				// Bildschirm sprengen und trotzdem per Pager blätterbar
				// bleiben (FR-Zoom).
				fitRows := l.height - 3
				if fitRows < 4 {
					fitRows = 4
				}
				img := NewImageBlock(r.png, strings.TrimSuffix(r.ascii, "\n"), cb.copyFn, cb.th)
				img = img.SetID(r.id).(ImageBlock).WithFitRows(fitRows)
				if cb.focused {
					img = img.SetFocused(true).(ImageBlock)
				}
				l.items[i] = img
				applied++
				break
			}
			nb := cb
			nb.converted = true
			if r.ok && strings.TrimSpace(r.ascii) != "" {
				nb.code = strings.TrimSuffix(r.ascii, "\n")
			}
			l.items[i] = nb
			applied++
			break
		}
	}
	return l, applied
}