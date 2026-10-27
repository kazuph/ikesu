package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/kazuph/ikesu/internal/layout"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
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
	atlas := image.NewRGBA(image.Rect(0, 0, 40*8, 16*len(KoiPatterns)*2))
	for v := range KoiPatterns {
		for d := 0; d < 2; d++ {
			for wag := 0; wag < 8; wag++ {
				sprite := KoiSprite(v, d, wag, 40, 16)
				at := image.Pt(wag*40, (v*2+d)*16)
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
			at := koiPixelPosition(fish, 8, 16)
			draw.Draw(img, image.Rectangle{Min: at, Max: at.Add(sprite.Bounds().Size())}, sprite, image.Point{}, draw.Over)
		}
		colors := color.Palette{}
		seen := map[color.RGBA]bool{}
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				c := img.RGBAAt(x, y)
				if !seen[c] {
					seen[c] = true
					colors = append(colors, c)
				}
			}
		}
		if len(colors) > 256 {
			t.Fatalf("GIF palette overflow: %d", len(colors))
		}
		frame := image.NewPaletted(img.Bounds(), colors)
		draw.Draw(frame, img.Bounds(), img, image.Point{}, draw.Src)
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

func TestExportKoiPixelProof(t *testing.T) {
	dest := os.Getenv("KOI_PIXEL_PROOF")
	if dest == "" {
		t.Skip("explicit pixel proof export only")
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	for _, part := range []struct {
		name string
		w, h int
	}{{"pond", 40, 16}, {"mini", 24, 8}, {"site", 40, 16}} {
		for _, scale := range []int{1, 2, 4} {
			img := KoiSprite(0, 0, 0, part.w*scale, part.h*scale)
			f, err := os.Create(filepath.Join(dest, fmt.Sprintf("%s-%dx.png", part.name, scale)))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}

func TestKoiBrandAtlasMatchesRendererAndEmbeddedSite(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "brand", "koi-atlas.png")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	atlas, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if atlas.Bounds().Dx() != 320 || atlas.Bounds().Dy() != 160 {
		t.Fatal("atlas grid changed", atlas.Bounds())
	}
	for v := range KoiPatterns {
		for d := 0; d < 2; d++ {
			for wag := 0; wag < 8; wag++ {
				sprite := KoiSprite(v, d, wag, 40, 16)
				for y := 0; y < 16; y++ {
					for x := 0; x < 40; x++ {
						got := color.RGBAModel.Convert(atlas.At(wag*40+x, (v*2+d)*16+y))
						if got != sprite.RGBAAt(x, y) {
							t.Fatal("atlas diverged from terminal renderer", v, d, wag, x, y)
						}
					}
				}
			}
		}
	}
	site, err := os.ReadFile(filepath.Join("..", "..", "site", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	prefix := "atlas.src='data:image/png;base64,"
	_, payload, ok := strings.Cut(string(site), prefix)
	if !ok {
		t.Fatal("site has no embedded atlas")
	}
	payload, _, ok = strings.Cut(payload, "'")
	if !ok {
		t.Fatal("unterminated atlas")
	}
	embedded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, embedded) {
		t.Fatal("site artwork differs from renderer atlas")
	}
}

func TestKoiGIFKeepsEnlargedDotGrid(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "brand", "koi.gif"))
	if err != nil {
		t.Fatal(err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	colors := map[color.RGBA]bool{}
	for v := range KoiPatterns {
		for wag := 0; wag < 8; wag++ {
			sprite := KoiPixelSprite(v, 0, wag, 40, 16)
			for y := 0; y < 16; y++ {
				for x := 0; x < 40; x++ {
					c := sprite.RGBAAt(x, y)
					if c.A != 0 {
						colors[c] = true
					}
				}
			}
		}
	}
	for index, frame := range animation.Image {
		for y := 0; y < frame.Bounds().Dy(); y += 2 {
			for x := 0; x < frame.Bounds().Dx(); x += 2 {
				var samples [4]color.RGBA
				fish := false
				for i := range samples {
					samples[i] = color.RGBAModel.Convert(frame.At(x+i%2, y+i/2)).(color.RGBA)
					fish = fish || colors[samples[i]]
				}
				if fish {
					for _, c := range samples[1:] {
						if c != samples[0] {
							t.Fatalf("GIF fish lost its doubled dot grid: frame=%d block=%d,%d", index, x, y)
						}
					}
				}
			}
		}
	}
}
