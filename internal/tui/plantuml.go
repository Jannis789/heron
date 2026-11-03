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
	svg   []byte // Vektor-Quelle (FR-27, Metrik 2): bei Resize neu rasern
	// src ist der rohe Zaun-Quelltext — er ueberlebt die Konvertierung und
	// landet im ImageBlock (SetSource): t schaltet dort zwischen Bild
	// (Standard) und Quelltext um. Ohne das Feld blieb nach der Konvertierung
	// nur die ASCII-Grafik (ascii) uebrig.
	src string
	ok  bool
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
			res := diagramResult{id: j.id, lang: j.lang, ascii: j.code, src: j.code}
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
					// Vektor zuerst: SVG + resvg rastert spaeter auf die
					// exakte Terminal-Zielgroesse (0x Skalierung). Ohne
					// resvg bleibt der bewaehrte -tpng-Pfad.
					if render.ResvgAvailable() {
						if svg, err := render.PlantUMLToSVG(ctx, j.code); err == nil {
							res.svg = svg
							res.ok = true
						} else {
							imageLogf("convert %s id=%d svg-fehler: %v", j.lang, j.id, err)
						}
					}
					if res.svg == nil {
						if png, err := render.PlantUMLToPNG(ctx, j.code); err == nil {
							res.png = png
							res.ok = true
						}
					}
				}
				if res.png == nil {
					if ascii, err := render.PlantUMLToASCII(ctx, j.code); err == nil {
						res.ascii = ascii
						res.ok = true
					}
				}
			}
			if res.svg != nil {
				imageLogf("convert %s id=%d ok svg=%d bytes (%.1fms)", j.lang, j.id, len(res.svg), float64(time.Since(t0).Microseconds())/1000.0)
			} else if res.png != nil {
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

// imageLogPath ist der Pfad des Diagnose-Logs. HERON_IMAGE_LOG uebersteuert
// ihn — ohne das koennen zwei gleichzeitige heron-Laufe (z. B. der echte
// Nutzer-Terminal und ein automatisierter Test) in dieselbe Datei schreiben
// und ihre Messwerte vermischen sich (gemessen: "win 337x59" und "win 80x24"
// in EINEM Log, dazu fremde kB/s-Zahlen). Getrennte Pfade = gueltige Messung.
func imageLogPath() string {
	if p := os.Getenv("HERON_IMAGE_LOG"); p != "" {
		return p
	}
	return "/tmp/heron-images.log"
}

func imageLogf(format string, a ...interface{}) {
	if !imageDebugOn() {
		return
	}
	imageLogMu.Lock()
	defer imageLogMu.Unlock()
	f, err := os.OpenFile(imageLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
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
func (l BlockList) applyDiagrams(results []diagramResult, useImg bool, caps TermCaps) (BlockList, int) {
	applied := 0
	// Sprungschutz VOR dem Ersetzen: Bild/ASCII hat eine andere Hoehe als
	// der Roh-Zaun, der Viewport darf beim Nachladen nicht wandern. Der
	// Anker ist block-relativ (stabil gegen Hoehenaenderung); follow haelt
	// den Pager wie bei Stream/Think (upsertStreamWidgets) unten fest.
	follow := l.follow
	anchorIdx, anchorRel := l.topAnchor()
	for _, r := range results {
		for i, it := range l.items {
			cb, ok := it.(CodeBlock)
			if !ok || cb.id != r.id {
				continue
			}
			if r.png != nil || r.svg != nil {
				// Kitty-Platzhalter: nur verwenden, wenn das Terminal sie kann.
				if !useImg {
					nb := cb
					nb.converted = true
					l.items[i] = nb
					applied++
					break
				}
				img := NewImageBlock(r.png, strings.TrimSuffix(r.ascii, "\n"), true, caps, cb.copyFn, cb.th)
				// Roh-Quelltext fuer den t-Switch (Bild <-> Text) mitgeben,
				// bevor ID/Fokus gesetzt werden — SetID/Focused propagieren.
				if r.src != "" {
					img = img.SetSource(r.lang, r.src).(ImageBlock)
				}
				// Terminal-Bild-ID: 1..250, in \e[38;5;N m codierbar (FR-27).
				if r.svg != nil {
					img = img.SetSVG(r.svg).(ImageBlock)
				}
				l.nextKittyID++
				if l.nextKittyID > kittyIDMax {
					l.nextKittyID = 1
				}
				img = img.SetKittyID(l.nextKittyID).(ImageBlock)
				img = img.SetID(r.id).(ImageBlock)
				// Wandelt man den Cap erst beim naechsten SetSize an, ist
				// das Bild bis dahin so hoch wie das ganze Fenster (kein
				// Text sichtbar) — also sofort begrenzen.
				cap := l.imgRowCap()
				imageLogf("cap block=%d chat-hoehe=%d cap=%d", r.id, l.height, cap)
				if cap > 0 {
					img = img.SetMaxRows(cap).(ImageBlock)
				}
				// lastWidth initialisieren (gleiche Lektion wie beim Cap):
				// zoomBy/panBy klemmen an frameWidth(), das nach der
				// Konvertierung noch 0 (Fallback 80) waere — der Render-Pfad
				// rechnet aber mit der echten Chat-Breite. Ohne Init zaehlt
				// der ⤢-Indikator bei breiten Diagrammen bis ~390%, waehrend
				// das virtuelle Raster laengst an der Diakritika-Grenze
				// stoppt: Zoom "klappt nicht mehr", der Indikator behauptet
				// das Gegenteil.
				img = img.Resize(l.width).(ImageBlock)
				if cb.focused {
					img = img.SetFocused(true).(ImageBlock)
				}
				// Markierung mitnehmen: focused propagiert bereits — ohne
				// das blieb die Auswahl unsichtbar im marks-Map haengen
				// (Chip weg, y kopiert sie trotzdem).
				if cb.marked {
					img = img.SetMarked(true).(ImageBlock)
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
	if follow {
		l = l.ScrollEnd()
	} else {
		l = l.restoreTopAnchor(anchorIdx, anchorRel)
	}
	return l, applied
}
