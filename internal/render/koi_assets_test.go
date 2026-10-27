package render

import (
	"github.com/kazuph/ikesu/internal/layout"
	"image"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Regenerate checked-in brand media explicitly; these use the shipped sprite
// and scene functions rather than a separate illustration implementation.
func TestExportKoiBrandAssets(t *testing.T) {
	dest := os.Getenv("KOI_BRAND_EXPORT")
	if dest == "" {
		t.Skip("explicit asset export only")
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	atlas := image.NewRGBA(image.Rect(0, 0, 80*8, 32*len(KoiPatterns)*2))
	for v := range KoiPatterns {
		for d := 0; d < 2; d++ {
			for wag := 0; wag < 8; wag++ {
				sprite := KoiSprite(v, d, wag, 80, 32)
				at := image.Pt(wag*80, (v*2+d)*32)
				draw.Draw(atlas, image.Rectangle{Min: at, Max: at.Add(sprite.Bounds().Size())}, sprite, image.Point{}, draw.Src)
			}
		}
	}
	f, err := os.Create(filepath.Join(dest, "koi-atlas.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, atlas); err != nil {
		t.Fatal(err)
	}
	f.Close()
	anim := &gif.GIF{LoopCount: 0}
	in := koiFixture(t, 5)
	for tick := 0; tick < 80; tick++ {
		in.KoiTime = float64(tick) / 20
		base := NewFrame(in.State.Cols, in.State.Rows)
		scene := KoiSceneFor(base, in, nil)
		img := KoiPondImage(320, 160, 0, 0, 0)
		for i, fish := range scene.Fish[:min(4, len(scene.Fish))] {
			fish = koiPose(fish.Agent, layout.Rect{X: 0, Y: i * 3, W: 40, H: 2}, 10, in.KoiTime)
			sprite := KoiSprite(fish.Agent.Seed%len(KoiPatterns), fish.Direction, fish.Wag, 80, 32)
			at := image.Pt(int(fish.X*8), int(fish.Y*16))
			draw.Draw(img, image.Rectangle{Min: at, Max: at.Add(sprite.Bounds().Size())}, sprite, image.Point{}, draw.Over)
		}
		frame := image.NewPaletted(img.Bounds(), palette.Plan9)
		draw.FloydSteinberg.Draw(frame, img.Bounds(), img, image.Point{})
		anim.Image = append(anim.Image, frame)
		anim.Delay = append(anim.Delay, 5)
	}
	f, err = os.Create(filepath.Join(dest, "koi.gif"))
	if err != nil {
		t.Fatal(err)
	}
	if err := gif.EncodeAll(f, anim); err != nil {
		t.Fatal(err)
	}
	f.Close()
}
