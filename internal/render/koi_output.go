package render

import (
	"bytes"
	"github.com/kazuph/ikesu/internal/kitty"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"
)

type koiSpriteKey struct{ Variety, Direction, Wag, W, H int }

var koiSpriteCache sync.Map

func koiSpriteCached(k koiSpriteKey) (*image.RGBA, kitty.Image) {
	if v, ok := koiSpriteCache.Load(k); ok {
		p := v.(koiRaster)
		return p.Pixels, p.Image
	}
	img := KoiSprite(k.Variety, k.Direction, k.Wag, k.W, k.H)
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	p := koiRaster{img, kitty.Image{ID: uint32(1 + (k.Variety*2+k.Direction)*8 + k.Wag), Format: 100, Width: k.W, Height: k.H, Data: b.Bytes()}}
	actual, _ := koiSpriteCache.LoadOrStore(k, p)
	p = actual.(koiRaster)
	return p.Pixels, p.Image
}

type koiRaster struct {
	Pixels *image.RGBA
	Image  kitty.Image
}

func drawKoiGraphics(f *Frame, scene KoiScene, in Input) {
	cw, ch := in.KoiCellW, in.KoiCellH
	if cw <= 0 || ch <= 0 {
		return
	}
	if len(scene.Fish) > 0 {
		water := KoiPondImage(1, 1, 0, 0, 0)
		_, img := waterKoiImage(water)
		f.Graphics = append(f.Graphics, Graphic{Pane: -3, Placement: kitty.Placement{Image: img, X: scene.Pond.X, Y: scene.Pond.Y, Cols: scene.Pond.W, Rows: scene.Pond.H, Z: 0}})
	}
	for group, fishList := range [][]KoiFish{scene.Fish, scene.Mini} {
		if len(fishList) == 0 {
			continue
		}
		w, h := fishList[0].Rect.W*8, fishList[0].Rect.H*16
		// Keep every frame as a source even while not placed. The encoder uploads
		// each image once and retires it only when this decoration becomes hidden.
		for v := 0; v < len(KoiPatterns)*3; v++ {
			for d := 0; d < 2; d++ {
				for wag := 0; wag < 8; wag++ {
					_, img := koiSpriteCached(koiSpriteKey{v, d, wag, w, h})
					f.GraphicsSources = append(f.GraphicsSources, Graphic{Pane: -1 - group, Placement: kitty.Placement{Image: img}})
				}
			}
		}
		for _, fish := range fishList {
			v := koiVariant(fish)
			_, img := koiSpriteCached(koiSpriteKey{v, fish.Direction, fish.Wag, w, h})
			px := int(fish.X * float64(cw))
			py := int(fish.Y * float64(ch))
			f.Graphics = append(f.Graphics, Graphic{Pane: -1 - group, Placement: kitty.Placement{Image: img, X: px / cw, Y: py / ch, OffsetX: px % cw, OffsetY: py % ch, Cols: fish.Rect.W, Rows: fish.Rect.H, Z: 1}})
		}
	}
}

func drawKoiOctants(f *Frame, fish KoiFish) {
	w, h := fish.Rect.W*2, fish.Rect.H*4
	sprite, _ := koiSpriteCached(koiSpriteKey{koiVariant(fish), fish.Direction, fish.Wag, w, h})
	for y := 0; y < fish.Rect.H; y++ {
		for x := 0; x < fish.Rect.W; x++ {
			cell := f.Cell(fish.Rect.X+x, fish.Rect.Y+y)
			if cell == nil || cell.Width != 1 || cell.Content != " " {
				continue
			}
			var bg color.Color = f.Palette.PanelBG
			if bg == nil {
				bg = waterBG
			}
			if cell.Style.Bg != nil {
				bg = cell.Style.Bg
			}
			block := image.NewRGBA(image.Rect(0, 0, 2, 4))
			draw.Draw(block, block.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
			draw.Draw(block, block.Bounds(), sprite, image.Pt(x*2, y*4), draw.Over)
			var px [8]color.RGBA
			visible := false
			for i := range px {
				px[i] = block.RGBAAt(i%2, i/2)
				visible = visible || sprite.RGBAAt(x*2+i%2, y*4+i/2).A != 0
			}
			if !visible {
				continue
			}
			fg, back, mask := twoColorOctant(px)
			style := cell.Style
			style.Fg, style.Bg = fg, back
			f.Set(fish.Rect.X+x, fish.Rect.Y+y, string(octantRunes[mask]), style)
		}
	}
}
func twoColorOctant(px [8]color.RGBA) (color.RGBA, color.RGBA, int) {
	dist := func(a, b color.RGBA) int {
		r, g, bl := int(a.R)-int(b.R), int(a.G)-int(b.G), int(a.B)-int(b.B)
		return r*r + g*g + bl*bl
	}
	a, b, best := 0, 0, -1
	for i := range px {
		for j := i + 1; j < len(px); j++ {
			if d := dist(px[i], px[j]); d > best {
				a, b, best = i, j, d
			}
		}
	}
	var sums [2][3]int
	var counts [2]int
	mask := 0
	for i, c := range px {
		k := 1
		if dist(c, px[a]) <= dist(c, px[b]) {
			k = 0
			mask |= 1 << i
		}
		sums[k][0] += int(c.R)
		sums[k][1] += int(c.G)
		sums[k][2] += int(c.B)
		counts[k]++
	}
	avg := func(k int) color.RGBA {
		n := max(1, counts[k])
		return color.RGBA{uint8(sums[k][0] / n), uint8(sums[k][1] / n), uint8(sums[k][2] / n), 255}
	}
	return avg(0), avg(1), mask
}

func koiVariant(f KoiFish) int {
	v := f.Agent.Seed % len(KoiPatterns)
	if f.Agent.Glow {
		return v + 2*len(KoiPatterns)
	}
	if f.Agent.State == "blocked" && f.Wag < 4 {
		return v + len(KoiPatterns)
	}
	return v
}

var koiWaterOnce sync.Once
var koiWater kitty.Image

func waterKoiImage(img *image.RGBA) (*image.RGBA, kitty.Image) {
	koiWaterOnce.Do(func() {
		var b bytes.Buffer
		_ = png.Encode(&b, img)
		koiWater = kitty.Image{ID: 1, Format: 100, Width: 1, Height: 1, Data: b.Bytes()}
	})
	return img, koiWater
}
