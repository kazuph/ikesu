package render

import (
	"bytes"
	"github.com/kazuph/ikesu/internal/kitty"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sync/atomic"
)

type koiSpriteKey struct{ Variety, Direction, Wag, W, H int }

var koiSpriteCache = koiCache[koiSpriteKey, koiRaster]{maxEntries: koiSpriteEntries, maxBytes: koiSpriteBytes}

func koiSpriteCached(k koiSpriteKey) (*image.RGBA, kitty.Image) {
	p := koiSpriteCache.getOrCreate(k, func() (koiRaster, int) {
		v := k.Variety % (3 * len(KoiPatterns))
		img := KoiSprite(v, k.Direction, k.Wag, k.W, k.H)
		if k.W < 2*k.H {
			img = koiPondSprite(v, k.Direction, k.Wag, k.W, k.H)
		}
		if k.Variety >= 3*len(KoiPatterns) {
			for i := 0; i < len(img.Pix); i += 4 {
				for c := 0; c < 3; c++ {
					img.Pix[i+c] = uint8(int(img.Pix[i+c]) * 3 / 4)
				}
			}
		}
		var b bytes.Buffer
		_ = png.Encode(&b, img)
		p := koiRaster{img, kitty.Image{ID: uint32(1 + (k.Variety*16+k.Direction)*8 + k.Wag), Format: 100, Width: k.W, Height: k.H, Data: b.Bytes()}}
		return p, len(img.Pix) + len(p.Image.Data)
	})
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
	for group, fishList := range [][]KoiFish{scene.Fish, scene.Mini} {
		if len(fishList) == 0 {
			continue
		}
		w, h := fishList[0].Rect.W*cw, fishList[0].Rect.H*ch
		for _, fish := range fishList {
			v := koiVariant(fish)
			_, img := koiSpriteCached(koiSpriteKey{v, fish.Direction, fish.Wag, w, h})
			at := koiPixelPosition(fish, cw, ch)
			px, py := at.X, at.Y
			f.Graphics = append(f.Graphics, Graphic{Pane: -1 - group, Placement: kitty.Placement{Image: img, X: px / cw, Y: py / ch, OffsetX: px % cw, OffsetY: py % ch, Cols: fish.Rect.W, Rows: fish.Rect.H, Z: 1 + int((3-fish.Depth)*100)}, Koi: true})
		}
	}
	drawKoiRippleGraphics(f, scene, in)
}

// koiPixelPosition keeps source dots aligned and the image inside its lane.
func koiPixelPosition(fish KoiFish, cw, ch int) image.Point {
	_, _, scale := koiPixelGrid(fish.Rect.W*cw, fish.Rect.H*ch)
	if fish.Rect.H >= 3 {
		scale = max(1, min(fish.Rect.W*cw/40, fish.Rect.H*ch/40))
	}
	dx := int(math.Round((fish.X - float64(fish.Lane.X)) * float64(cw) / float64(scale)))
	dy := int(math.Round((fish.Y - float64(fish.Lane.Y)) * float64(ch) / float64(scale)))
	px := fish.Lane.X*cw + max(0, min(dx, (fish.Lane.W-fish.Rect.W)*cw/scale))*scale
	py := fish.Lane.Y*ch + max(0, min(dy, (fish.Lane.H-fish.Rect.H)*ch/scale))*scale
	return image.Pt(px, py)
}

func drawKoiOctants(f *Frame, fish KoiFish) {
	if fish.Rect.H < 3 {
		drawKoiRowOctants(f, fish)
		return
	}
	source, _ := koiSpriteCached(koiSpriteKey{koiVariant(fish), fish.Direction, fish.Wag, fish.Rect.W * 2, fish.Rect.H * 4})
	// Translate on the pond's 2x4-dot plane before packing cells, including
	// the final partial cell at either edge of a moving sprite.
	ox, oy := int(math.Round(fish.X*2)), int(math.Round(fish.Y*4))
	x0, y0 := max(fish.Lane.X, int(math.Floor(float64(ox)/2))), max(fish.Lane.Y, int(math.Floor(float64(oy)/4)))
	x1, y1 := min(fish.Lane.X+fish.Lane.W, (ox+source.Bounds().Dx()+1)/2), min(fish.Lane.Y+fish.Lane.H, (oy+source.Bounds().Dy()+3)/4)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			cell := f.Cell(x, y)
			if cell == nil || cell.Width != 1 {
				continue
			}
			oldMask := -1
			if cell.Content != " " {
				for i, r := range octantRunes {
					if cell.Content == string(r) {
						oldMask = i
						break
					}
				}
				if oldMask < 0 {
					continue
				}
			}
			bg := cell.Style.Bg
			if bg == nil {
				bg = f.Palette.PanelBG
			}
			if bg == nil {
				bg = waterBG
			}
			var px [8]color.RGBA
			visible := false
			for i := range px {
				previous := bg
				if oldMask >= 0 && oldMask&(1<<i) != 0 && cell.Style.Fg != nil {
					previous = cell.Style.Fg
				}
				px[i] = color.RGBAModel.Convert(previous).(color.RGBA)
				c := source.RGBAAt(x*2+i%2-ox, y*4+i/2-oy)
				if c.A != 0 {
					px[i] = c
					visible = true
				}
			}
			if !visible {
				continue
			}
			fg, back, mask := twoColorOctant(px)
			background := color.RGBAModel.Convert(bg).(color.RGBA)
			hasBackground := false
			counts := map[color.RGBA]int{}
			for _, c := range px {
				hasBackground = hasBackground || c == background
				if c != background {
					counts[c]++
				}
			}
			if hasBackground {
				back = background
				best := 0
				for _, c := range px {
					if c != background && counts[c] > best {
						fg = c
						best = counts[c]
					}
				}
				mask = 0
				for i, c := range px {
					if c != background {
						mask |= 1 << i
					}
				}
			}
			style := cell.Style
			style.Fg, style.Bg = fg, back
			f.Set(x, y, string(octantRunes[mask]), style)
		}
	}
}

func drawKoiRowOctants(f *Frame, fish KoiFish) {
	w, h := fish.Rect.W*2, fish.Rect.H*4
	source, _ := koiSpriteCached(koiSpriteKey{koiVariant(fish), fish.Direction, fish.Wag, w, h})
	rows := fish.Rect.H
	if fish.Rect.H == 2 {
		rows = max(rows, fish.Lane.H)
	}
	sprite := image.NewRGBA(image.Rect(0, 0, w, rows*4))
	offset := min(max(0, int(math.Round((fish.Y-float64(fish.Rect.Y))*4))), rows*4-h)
	draw.Draw(sprite, source.Bounds().Add(image.Pt(0, offset)), source, image.Point{}, draw.Src)
	for y := 0; y < rows; y++ {
		for x := 0; x < fish.Rect.W; x++ {
			cell := f.Cell(fish.Rect.X+x, fish.Rect.Y+y)
			if cell == nil || cell.Width != 1 {
				continue
			}
			if cell.Content != " " {
				found := false
				for _, r := range octantRunes {
					found = found || cell.Content == string(r)
				}
				if !found {
					continue
				}
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
	mask := 0
	for i, c := range px {
		if dist(c, px[a]) <= dist(c, px[b]) {
			mask |= 1 << i
		}
	}

	return px[a], px[b], mask
}

func koiVariant(f KoiFish) int {
	v := f.Agent.Seed % len(KoiPatterns)
	if f.Agent.Glow {
		return v + 2*len(KoiPatterns)
	}
	if f.Agent.State == "blocked" && f.Wag < 4 {
		return v + len(KoiPatterns)
	}
	if f.Depth >= 1 {
		return v + 3*len(KoiPatterns)
	}
	return v
}

var koiRingCache = koiCache[koiRingKey, kitty.Image]{maxEntries: koiRingEntries, maxBytes: koiRingBytes}
var koiRingID atomic.Uint32

func koiRippleCached(key koiRingKey) kitty.Image {
	return koiRingCache.getOrCreate(key, func() (kitty.Image, int) {
		img := koiRing(key)
		var b bytes.Buffer
		_ = png.Encode(&b, img)
		result := kitty.Image{ID: koiRingID.Add(1), Format: 100, Width: key.W, Height: key.H, Data: b.Bytes()}
		return result, len(result.Data)
	})
}
