package render

import (
	"image"
	"image/color"
	"math"
)

// These are sprite design dimensions, not agent-count or behavior limits.
const koiPixelWidth, koiPixelHeight = 40, 16

var koiOutline = color.RGBA{0x17, 0x2b, 0x2a, 255}

// KoiPixelSprite samples the shared variety shapes on a low-resolution grid,
// quantizes to eight fixed colors, then adds a one-dot dark silhouette.
func KoiPixelSprite(variety, direction, wag, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	p := KoiPatterns[variety%len(KoiPatterns)]
	palette := []color.RGBA{koiOutline, p.Base, p.Spot, p.Spot2, {0x8a, 0xa3, 0x96, 255}, {0xd3, 0xdd, 0xc7, 255}, {0xff, 0xef, 0xc3, 255}, {0x9f, 0x77, 0x3a, 255}}
	// Missing spot colors are intentionally replaced by the variety's base.
	for i := range palette {
		if palette[i].A == 0 {
			palette[i] = p.Base
		}
		palette[i].A = 255
	}
	if variety >= 2*len(KoiPatterns) {
		palette = []color.RGBA{koiOutline, {0xf4, 0xc5, 0x4d, 255}, {0xdc, 0x94, 0x29, 255}, {0x93, 0x57, 0x25, 255}, {0xa6, 0x8a, 0x43, 255}, {0xf8, 0xe1, 0x8d, 255}, {0xff, 0xf3, 0xbd, 255}, {0xbb, 0x73, 0x25, 255}}
	}
	nearest := func(c color.RGBA) color.RGBA {
		best := math.MaxInt
		result := palette[0]
		for _, q := range palette {
			r, g, b := int(c.R)-int(q.R), int(c.G)-int(q.G), int(c.B)-int(q.B)
			d := r*r + g*g + b*b
			if d < best {
				best, result = d, q
			}
		}
		return result
	}
	shape := image.NewRGBA(img.Bounds())
	length := float64(w - 4)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			u := (float64(x)-float64(w)/2)/length + .5
			v := (float64(y) - float64(h)/2) / length
			c, a := koiSample(p, u, v, math.Sin(float64(wag)/8*2*math.Pi))
			if a > 0 {
				shape.SetRGBA(x, y, nearest(c))
			}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := shape.RGBAAt(x, y)
			if c.A == 0 {
				for _, d := range []image.Point{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
					if shape.RGBAAt(x+d.X, y+d.Y).A != 0 {
						c = koiOutline
						break
					}
				}
			}
			dx := x
			if direction == 1 {
				dx = w - 1 - x
			}
			img.SetRGBA(dx, y, c)
		}
	}
	// Two state frames: a dark open mouth, or a closed, outlined head.
	if variety >= len(KoiPatterns) && variety < 2*len(KoiPatterns) {
		x := w - 3
		if direction == 1 {
			x = 2
		}
		img.SetRGBA(x, h/2, koiOutline)
		img.SetRGBA(x-1+2*direction, h/2, koiOutline)
	}
	return img
}

func koiPixelGrid(w, h int) (int, int, int) {
	bw, bh := koiPixelWidth, koiPixelHeight
	if w >= h*3 {
		bw, bh = 24, 8
	}
	return bw, bh, max(1, min(w/bw, h/bh))
}

// KoiSprite pads an integer nearest-neighbor enlargement to the terminal box.
// No sampling creates intermediate colors, including the fins and state frames.
func KoiSprite(variety, direction, wag, w, h int) *image.RGBA {
	bw, bh, scale := koiPixelGrid(w, h)
	source := KoiPixelSprite(variety, direction, wag, bw, bh)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	ox, oy := (w-bw*scale)/2, (h-bh*scale)/2
	if w < bw || h < bh {
		// Octants sample that same source at their native 2x4-dot cell grid.
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.SetRGBA(x, y, source.RGBAAt(x*bw/w, y*bh/h))
			}
		}
		return out
	}
	for y := 0; y < bh*scale; y++ {
		for x := 0; x < bw*scale; x++ {
			out.SetRGBA(ox+x, oy+y, source.RGBAAt(x/scale, y/scale))
		}
	}
	return out
}

// Pond frames rotate nearest-neighbor samples of the unchanged 40x16 artwork
// into a square source; each direction retains the original fixed palette.
func koiPondSprite(variety, direction, wag, w, h int) *image.RGBA {
	source := KoiPixelSprite(variety, 0, wag, 40, 16)
	square := image.NewRGBA(image.Rect(0, 0, 40, 40))
	angle := float64(direction) * 2 * math.Pi / 16
	c, s := math.Cos(angle), math.Sin(angle)
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			dx, dy := float64(x)-19.5, float64(y)-19.5
			sx, sy := int(math.Round(c*dx+s*dy+19.5)), int(math.Round(-s*dx+c*dy+7.5))
			if sx >= 0 && sy >= 0 && sx < 40 && sy < 16 {
				square.SetRGBA(x, y, source.RGBAAt(sx, sy))
			}
		}
	}
	if w < 40 || h < 40 {
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.SetRGBA(x, y, square.RGBAAt(x*40/w, y*40/h))
			}
		}
		return out
	}
	scale := max(1, min(w/40, h/40))
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	ox, oy := (w-40*scale)/2, (h-40*scale)/2
	for y := 0; y < 40*scale; y++ {
		for x := 0; x < 40*scale; x++ {
			out.SetRGBA(x+ox, y+oy, square.RGBAAt(x/scale, y/scale))
		}
	}
	return out
}
