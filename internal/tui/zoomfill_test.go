package tui

import "testing"

// Wunschszenario 10:N: Beim Zoomen klemmt aspectFillCrop den sichtbaren
// Ausschnitt exakt auf den Box-Aspekt — der enthaltene Ausschnitt faellt
// nach der contain-Skalierung des Terminals weder rechts freie Spalten noch
// unten leere Zeilen, bei JEDER Zoomstufe. Die Box waechst dabei mit dem
// Zoom von der natuerlichen (100%)-Breite bis max. auf die Rahmeninnenbreite
// (width-4) — "max ist immer das Fenster". Die Hoehe N ist frei.
func TestZoomCroppedRegionFillsBox(t *testing.T) {
	caps := TermCaps{CellW: 9, CellH: 20}
	scenarios := []struct {
		name        string
		pw, ph, wid int
	}{
		{"seq-diagramm (2.26:1)", 1437, 635, 250},
		{"seq-374 (sehr breites Terminal)", 1437, 635, 374},
		{"breit (7.5:1)", 3000, 400, 200},
		{"portrait", 377, 598, 120},
		{"quadrat", 500, 500, 120},
	}
	for _, sc := range scenarios {
		raw := NewImageBlock(realPNG(sc.pw, sc.ph), "roh", true, caps, nil, nil)
		raw = raw.SetID(1).(ImageBlock).SetKittyID(1).(ImageBlock)
		nb := raw.Resize(sc.wid).(ImageBlock).SetMaxRows(35).(ImageBlock)
		nb.panning = true
		for _, z := range []int{125, 150, 200, 300, 400} {
			nb.zoom = z
			vC, vR, wC, wR, pR, pC := nb.zoomLayout(sc.wid)
			if wC > sc.wid-4 {
				t.Errorf("%s z=%d: Box-Breite %d > max Rahmenbreite %d", sc.name, z, wC, sc.wid-4)
			}
			// Kein Diakritika-297-Cap mehr: die Bildzeile ist per Spalten-
			// Omission beliebig breit; nur die Rahmenbreite begrenzt.
			if wC > sc.wid-4 {
				t.Errorf("%s z=%d: Box-Breite %d > max Rahmenbreite %d", sc.name, z, wC, sc.wid-4)
			}
			cr := cropRect(sc.pw, sc.ph, vC, vR, wC, wR, pR, pC)
			fix := aspectFillCrop(cr, sc.pw, sc.ph, wC, wR, caps.CellW, caps.CellH)
			gapSp, gapZe := containGaps(fix.Dx(), fix.Dy(), wC, wR, caps.CellW, caps.CellH)
			if gapSp != 0 || gapZe != 0 {
				t.Errorf("%s z=%d: Box=%dx%d nat=%v fix=%v — ungenutzt rechts=%d Spalten, unten=%d Zeilen",
					sc.name, z, wC, wR, cr, fix, gapSp, gapZe)
			}
		}
	}
}

func containGaps(px, py, cols, rows, cellW, cellH int) (int, int) {
	boxW, boxH := cols*cellW, rows*cellH
	sW := float64(boxW) / float64(px)
	sH := float64(boxH) / float64(py)
	s := sW
	if sH < s {
		s = sH
	}
	lw := boxW - int(float64(px)*s)
	lh := boxH - int(float64(py)*s)
	return lw / cellW, lh / cellH
}
