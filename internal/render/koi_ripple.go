package render

import (
	"github.com/kazuph/ikesu/internal/kitty"
	"image"
	"image/color"
	"math"
)

// Rings use the same integer source grid and cached image path as fish.
func koiRippleFish(scene KoiScene) (KoiFish, bool) {
	r := scene.Ripple
	if r == nil || r.Time-r.Start >= KoiRippleLifetime {
		return KoiFish{}, false
	}
	stage := int((r.Time - r.Start) / KoiInterval.Seconds())
	return KoiFish{Rect: scene.Pond, Wag: stage}, true
}

type koiRingKey struct{ W, H, X, Y, Radius int }

func koiRingGeometry(scene KoiScene, cw, ch int) koiRingKey {
	scale := max(1, min(10*cw/40, 5*ch/40))
	w, h := scene.Pond.W*cw/scale, scene.Pond.H*ch/scale
	x, y := int(math.Round((scene.Ripple.X-float64(scene.Pond.X))*float64(cw)/float64(scale))), int(math.Round((scene.Ripple.Y-float64(scene.Pond.Y))*float64(ch)/float64(scale)))
	far := math.Hypot(float64(max(x, w-x)), float64(max(y, h-y)))
	stages := int(KoiRippleLifetime / KoiInterval.Seconds())
	stage := min(stages, int((scene.Ripple.Time-scene.Ripple.Start)/KoiInterval.Seconds())+1)
	return koiRingKey{w, h, x, y, int(math.Ceil(far * float64(stage) / float64(stages)))}
}

func koiRing(k koiRingKey) *image.Paletted {
	// Rings have exactly two colors. Palette PNGs preserve those pixels while
	// avoiding per-pixel RGBA conversion and five full-width filter candidates.
	out := image.NewPaletted(image.Rect(0, 0, k.W, k.H), color.Palette{color.RGBA{}, color.RGBA{0x8a, 0xa3, 0x96, 255}})
	for y := 0; y < k.H; y++ {
		for x := 0; x < k.W; x++ {
			if math.Abs(math.Hypot(float64(x-k.X), float64(y-k.Y))-float64(k.Radius)) < .5 {
				out.SetColorIndex(x, y, 1)
			}
		}
	}
	return out
}
func drawKoiRippleGraphics(f *Frame, scene KoiScene, in Input) {
	fish, ok := koiRippleFish(scene)
	if !ok {
		return
	}
	img := koiRippleCached(koiRingGeometry(scene, in.KoiCellW, in.KoiCellH))
	g := Graphic{Koi: true, Pane: -4, Placement: kitty.Placement{Image: img, X: fish.Rect.X, Y: fish.Rect.Y, Cols: fish.Rect.W, Rows: fish.Rect.H, Z: 400}}
	visible := image.Rect(fish.Rect.X, fish.Rect.Y, fish.Rect.X+fish.Rect.W, fish.Rect.Y+fish.Rect.H).Intersect(image.Rect(scene.Pond.X, scene.Pond.Y, scene.Pond.X+scene.Pond.W, scene.Pond.Y+scene.Pond.H))
	if !visible.Empty() {
		f.Graphics = append(f.Graphics, cropGraphic(g, visible))
	}
}
func drawKoiRippleOctants(f *Frame, scene KoiScene) {
	fish, ok := koiRippleFish(scene)
	if !ok {
		return
	}
	img := koiRing(koiRingGeometry(scene, 2, 4))
	for cy := 0; cy < fish.Rect.H; cy++ {
		for cx := 0; cx < fish.Rect.W; cx++ {
			c := f.Cell(fish.Rect.X+cx, fish.Rect.Y+cy)
			if c == nil || c.Content != " " {
				continue
			}
			mask := 0
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					if img.ColorIndexAt(cx*2+dx, cy*4+dy) != 0 {
						mask |= 1 << (dy*2 + dx)
					}
				}
			}
			if mask != 0 {
				style := c.Style
				style.Fg = color.RGBA{0x8a, 0xa3, 0x96, 255}
				f.Set(fish.Rect.X+cx, fish.Rect.Y+cy, string(octantRunes[mask]), style)
			}
		}
	}
}
