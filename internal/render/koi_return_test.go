package render

import (
	"bytes"
	"github.com/kazuph/ikesu/internal/layout"
	"image/png"
	"testing"
)

func TestKoiReturnRippleReachesEveryPondEdge(t *testing.T) {
	pond := layout.Rect{X: 130, Y: 10, W: 29, H: 40}
	scene := KoiScene{Pond: pond}
	edges := [4]bool{}
	for tick := 0; tick < int(KoiRippleLifetime/KoiInterval.Seconds()); tick++ {
		scene.Ripple = &KoiRipple{X: 144, Y: 30, Time: float64(tick) * KoiInterval.Seconds()}
		frame := NewFrame(160, 60)
		drawKoiRippleGraphics(frame, scene, Input{KoiCellW: 8, KoiCellH: 16})
		for _, g := range frame.Graphics {
			img, err := png.Decode(bytes.NewReader(g.Placement.Image.Data))
			if err != nil {
				t.Fatal(err)
			}
			p := g.Placement
			for y := 0; y < img.Bounds().Dy(); y++ {
				for x := 0; x < img.Bounds().Dx(); x++ {
					_, _, _, alpha := img.At(x, y).RGBA()
					if alpha == 0 {
						continue
					}
					px := p.X*8 + x*p.Cols*8/img.Bounds().Dx()
					py := p.Y*16 + y*p.Rows*16/img.Bounds().Dy()
					edges[0] = edges[0] || px <= pond.X*8+2
					edges[1] = edges[1] || px >= (pond.X+pond.W)*8-3
					edges[2] = edges[2] || py <= pond.Y*16+2
					edges[3] = edges[3] || py >= (pond.Y+pond.H)*16-3
				}
			}
		}
	}
	for side, hit := range edges {
		if !hit {
			t.Errorf("ripple never reached pond edge %d; touches=%v", side, edges)
		}
	}
}

func TestKoiReturnRepeatedRippleReusesTerminalImages(t *testing.T) {
	scene := KoiScene{Pond: layout.Rect{X: 130, Y: 10, W: 29, H: 40}}
	var enc Encoder
	for repeat := 0; repeat < 2; repeat++ {
		for tick := 0; tick < int(KoiRippleLifetime/KoiInterval.Seconds()); tick++ {
			scene.Ripple = &KoiRipple{X: 144, Y: 30, Time: float64(tick) * KoiInterval.Seconds()}
			frame := NewFrame(160, 60)
			drawKoiRippleGraphics(frame, scene, Input{KoiCellW: 8, KoiCellH: 16})
			data := enc.KoiFrame(frame, 0, 0, false)
			if repeat == 1 && bytes.Contains(data, []byte("a=t,")) {
				t.Fatalf("same ripple picture uploaded again at frame %d", tick)
			}
		}
		enc.KoiFrame(NewFrame(160, 60), 0, 0, false)
	}
}
