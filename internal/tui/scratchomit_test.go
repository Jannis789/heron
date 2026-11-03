package tui

import (
	"testing"
)

func TestProbeOmission(t *testing.T) {
	caps := TermCaps{CellW: 9, CellH: 20}
	width := 374
	raw := NewImageBlock(realPNG(1437, 635), "roh", true, caps, nil, nil)
	raw = raw.SetID(1).(ImageBlock).SetKittyID(1).(ImageBlock)
	nb := raw.Resize(width).(ImageBlock).SetMaxRows(35).(ImageBlock)
	for _, z := range []int{100, 150, 200, 300, 400} {
		n := nb
		n.panning = true
		n.zoom = z
		vC, vR, wC, wR, pR, pC := n.zoomLayout(width)
		cr := cropRect(1437, 635, vC, vR, wC, wR, pR, pC)
		fix := aspectFillCrop(cr, 1437, 635, wC, wR, caps.CellW, caps.CellH)
		gw, gh := containGapsXX(fix.Dx(), fix.Dy(), wC, wR, caps.CellW, caps.CellH)
		t.Logf("z=%3d Box=%dx%d (frei rechts=%d) fix=%v Luecken %dSp/%dZ", z, wC, wR, width-4-wC, fix, gw, gh)
	}
}
func containGapsXX(px, py, cols, rows, cellW, cellH int) (int, int) {
	boxW, boxH := cols*cellW, rows*cellH
	sW := float64(boxW) / float64(px)
	sH := float64(boxH) / float64(py)
	s := sW
	if sH < s {
		s = sH
	}
	return (boxW - int(float64(px)*s)) / cellW, (boxH - int(float64(py)*s)) / cellH
}
