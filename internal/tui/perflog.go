package tui

import (
	"sync"
	"time"
)

// Perf-Diagnose (HERON_IMAGE_DEBUG=1, FR-27 Metrik 1+3): misst, wie schnell
// heron selbst rendert — Frame-Rate, View-Dauer, Bytes. Ist die Go-Seite
// schnell und der Nutzer sieht trotzdem Ruckler, liegt die Zeit im
// Terminal-Compositing (z. B. Ghostty-Shaping der Platzhalterzellen),
// nicht in heron.
var perf = struct {
	sync.Mutex
	nView      int
	sumView    time.Duration
	maxView    time.Duration
	sumBytes   int
	lastSum    time.Time
	lastScroll time.Time
	scrolls    int
	// Raster-Zahler: wie oft resvg IM Render-Pfad laeuft (soll 0 sein —
	// 28 ms pro Aufruf im View sind die Lag-Quelle, FR-27 Metrik 1).
	rasterN  int
	rasterMs float64
}{}

// perfView erfasst eine View-Dauer und loggt pro Sekunde eine Zeile.
func perfView(d time.Duration, w, h int) {
	perf.Lock()
	defer perf.Unlock()
	if perf.lastSum.IsZero() {
		perf.lastSum = time.Now()
	}
	perf.nView++
	perf.sumView += d
	if d > perf.maxView {
		perf.maxView = d
	}
	if el := time.Since(perf.lastSum); el >= time.Second {
		avg := time.Duration(0)
		if perf.nView > 0 {
			avg = perf.sumView / time.Duration(perf.nView)
		}
		fps := float64(perf.nView) / el.Seconds()
		kb := perf.sumBytes / 1024
		if perf.rasterN > 0 {
			imageLogf("perf: raster IM render-pfad %dx (%.0f ms gesamt)", perf.rasterN, perf.rasterMs)
		}
		perf.rasterN, perf.rasterMs = 0, 0
		imageLogf("perf: %.0f frames/s | view avg=%.1fms max=%.1fms | %d kB/s | scrolls=%d | win %dx%d",
			fps, float64(avg.Microseconds())/1000, float64(perf.maxView.Microseconds())/1000, kb, perf.scrolls, w, h)
		perf.nView, perf.sumView, perf.maxView, perf.sumBytes = 0, 0, 0, 0
		perf.scrolls = 0
		perf.lastSum = time.Now()
	}
}

// perfRastered bucht einen resvg-Aufruf, der im Render-Pfad stattfand.
func perfRastered(ms float64) {
	perf.Lock()
	perf.rasterN++
	perf.rasterMs += ms
	perf.Unlock()
}

// perfBytes erfasst die Framegroesse (wenn View sie kennt).
func perfBytes(n int) {
	perf.Lock()
	perf.sumBytes += n
	perf.Unlock()
}

// perfScroll protokolliert Scroll-Ereignisse inkl. Abstand: zeigt, ob
// Events geballt ankommen (Wheel-Repeat) und wie schnell darauf gerendert
// wird.
func perfScroll(kind string, from, to int) {
	perf.Lock()
	dt := time.Since(perf.lastScroll)
	perf.lastScroll = time.Now()
	perf.scrolls++
	perf.Unlock()
	imageLogf("scroll: %s offset %d -> %d (+%.0fms seit letztem Scroll)", kind, from, to, float64(dt.Microseconds())/1000)
}
