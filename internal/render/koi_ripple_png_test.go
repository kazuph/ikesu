package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
)

func TestKoiRingPNGPreservesExactPixels(t *testing.T) {
	// First-frame geometry from the latency fixture, edge clicks, clipping,
	// and later radii must decode to the original RGBA raster, pixel for pixel.
	for _, k := range []koiRingKey{{116, 336, 58, 172, 6}, {116, 336, 0, 0, 6}, {116, 336, 115, 335, 100}, {116, 336, 58, 172, 400}, {16, 16, -2, 8, 12}} {
		original := image.NewRGBA(image.Rect(0, 0, k.W, k.H))
		for y := 0; y < k.H; y++ {
			for x := 0; x < k.W; x++ {
				if math.Abs(math.Hypot(float64(x-k.X), float64(y-k.Y))-float64(k.Radius)) < .5 {
					original.SetRGBA(x, y, color.RGBA{0x8a, 0xa3, 0x96, 255})
				}
			}
		}
		encoded := koiRippleCached(k)
		decoded, err := png.Decode(bytes.NewReader(encoded.Data))
		if err != nil {
			t.Fatal(err)
		}
		if encoded.Format != 100 || encoded.Width != k.W || encoded.Height != k.H || decoded.Bounds() != original.Bounds() {
			t.Fatalf("changed PNG geometry: %+v", k)
		}
		for y := 0; y < k.H; y++ {
			for x := 0; x < k.W; x++ {
				got := color.RGBAModel.Convert(decoded.At(x, y)).(color.RGBA)
				if want := original.RGBAAt(x, y); got != want {
					t.Fatalf("key=%+v pixel=(%d,%d) got=%v want=%v", k, x, y, got, want)
				}
			}
		}
		t.Logf("key=%+v pixels=%d PNG bytes=%d", k, k.W*k.H, len(encoded.Data))
	}
}
