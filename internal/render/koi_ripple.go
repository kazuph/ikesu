package render

import (
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/layout"
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
	stage := int((r.Time - r.Start) / KoiRippleLifetime * 8)
	return KoiFish{Rect: layout.Rect{X: int(r.X) - 5, Y: int(r.Y) - 2, W: 10, H: 5}, Wag: stage}, true
}
func koiRing(w, h, stage int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx := (float64(x)+.5)/float64(w)*40 - 20
			dy := (float64(y)+.5)/float64(h)*40 - 20
			radius := float64(stage+1) * 20 / 8
			if math.Abs(math.Hypot(dx, dy)-radius) < .5 {
				out.SetRGBA(x, y, color.RGBA{0x8a, 0xa3, 0x96, 255})
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
	img := koiRippleCached(fish.Wag)
	g := Graphic{Koi: true, Pane: -4, Placement: kitty.Placement{Image: img, X: fish.Rect.X, Y: fish.Rect.Y, Cols: 10, Rows: 5, Z: 400}}
	visible := image.Rect(fish.Rect.X, fish.Rect.Y, fish.Rect.X+10, fish.Rect.Y+5).Intersect(image.Rect(scene.Pond.X, scene.Pond.Y, scene.Pond.X+scene.Pond.W, scene.Pond.Y+scene.Pond.H))
	if !visible.Empty() {
		f.Graphics = append(f.Graphics, cropGraphic(g, visible))
	}
}
func drawKoiRippleOctants(f *Frame, scene KoiScene) {
	fish, ok := koiRippleFish(scene)
	if !ok {
		return
	}
	img := koiRing(20, 20, fish.Wag)
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			if img.RGBAAt(x, y).A == 0 {
				continue
			}
			cx, cy := fish.Rect.X+x/2, fish.Rect.Y+y/4
			if cx < scene.Pond.X || cy < scene.Pond.Y || cx >= scene.Pond.X+scene.Pond.W || cy >= scene.Pond.Y+scene.Pond.H {
				continue
			}
			c := f.Cell(cx, cy)
			if c == nil || c.Content != " " {
				continue
			}
			style := c.Style
			style.Fg = img.RGBAAt(x, y)
			f.Set(cx, cy, string(octantRunes[1<<((y%4)*2+x%2)]), style)
		}
	}
}
