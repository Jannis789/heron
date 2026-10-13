package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func mkBenchPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func BenchmarkPNGDims(b *testing.B) {
	p := mkBenchPNG(2000, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PNGDims(p)
	}
}

func BenchmarkPNGToANSI(b *testing.B) {
	p := mkBenchPNG(2000, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PNGToANSI(p, 80)
	}
}
