package render

// SPIKE (feat/koi-branding): resolution-independent koi seen from above.
// The same drawing feeds every output method: half blocks (1x2 dots per
// cell), octants (2x4 dots per cell, two colors) and real images.

import (
	"image"
	"image/color"
	"math"
)

type KoiPattern struct {
	Base, Spot, Spot2 color.RGBA
	Spots             [][3]float64 // position along spine (0 tail..1 head), side offset, radius (body widths)
	Spots2            [][3]float64
	Metallic          bool
}

var KoiPatterns = []KoiPattern{
	{ // kohaku: white with scarlet patches
		Base: color.RGBA{0xf7, 0xf3, 0xec, 0xff}, Spot: color.RGBA{0xd9, 0x2b, 0x1c, 0xff},
		Spots: [][3]float64{{0.83, 0, 0.55}, {0.58, 0.15, 0.6}, {0.36, -0.1, 0.5}},
	},
	{ // taisho sanke: white, red and small black marks
		Base: color.RGBA{0xf7, 0xf3, 0xec, 0xff}, Spot: color.RGBA{0xd4, 0x32, 0x1e, 0xff}, Spot2: color.RGBA{0x1c, 0x1a, 0x1a, 0xff},
		Spots:  [][3]float64{{0.78, 0.05, 0.5}, {0.45, -0.1, 0.55}},
		Spots2: [][3]float64{{0.62, 0.45, 0.22}, {0.3, -0.4, 0.2}, {0.5, 0.0, 0.16}},
	},
	{ // yamabuki ogon: metallic gold
		Base: color.RGBA{0xf2, 0xb2, 0x2e, 0xff}, Metallic: true,
	},
	{ // showa: black base with red and white
		Base: color.RGBA{0x22, 0x1f, 0x1f, 0xff}, Spot: color.RGBA{0xd0, 0x30, 0x1c, 0xff}, Spot2: color.RGBA{0xf2, 0xee, 0xe6, 0xff},
		Spots:  [][3]float64{{0.8, 0.1, 0.5}, {0.5, -0.2, 0.45}},
		Spots2: [][3]float64{{0.65, 0.3, 0.3}, {0.32, 0.25, 0.3}},
	},
	{ // tancho: white with a red crown only
		Base: color.RGBA{0xf7, 0xf3, 0xec, 0xff}, Spot: color.RGBA{0xe0, 0x1f, 0x18, 0xff},
		Spots: [][3]float64{{0.86, 0, 0.42}},
	},
}

// koiSample returns the color and coverage of a point in koi space: u runs
// tail (0) to head (1) along the length, v is the side offset in half body
// lengths. wag in [-1,1] bends the tail.
func koiSample(p KoiPattern, u, v, wag float64) (color.RGBA, float64) {
	// Bend: the spine shifts sideways more toward the tail.
	bend := wag * 0.09 * math.Pow(1-u, 2.2)
	v -= bend
	const half = 0.115 // max half width relative to length
	fin := color.RGBA{0xe8, 0xe2, 0xd8, 0xff}
	if p.Metallic {
		fin = color.RGBA{0xf6, 0xcf, 0x6a, 0xff}
	}
	// Tail fan.
	if u < 0.2 {
		t := (0.2 - u) / 0.2
		if u > -0.02 && math.Abs(v) < 0.025+t*0.13 && !(t > 0.7 && math.Abs(v) < (t-0.7)*0.3) {
			return fin, 0.6
		}
	}
	// Pectoral fins, both sides.
	for _, s := range []float64{1, -1} {
		du, dv := (u-0.66)/0.07, (v-s*0.14)/0.07
		rot := du*0.6 + dv*0.8*s
		if du*du+dv*dv < 1 && rot < 0.6 {
			return fin, 0.55
		}
	}
	if u < 0.12 || u > 1 {
		return color.RGBA{}, 0
	}
	// Torpedo outline, blunt head.
	x := (u - 0.12) / 0.88
	// Tapered tail, widest just behind the fins, then a round blunt head.
	const peak = 0.62
	var w float64
	if x < peak {
		w = half * math.Pow(math.Sin(math.Pi/2*x/peak), 0.8)
	} else {
		w = half * math.Sqrt(math.Max(0, 1-math.Pow((x-peak)/(1-peak), 2.4)))
	}
	if math.Abs(v) > w {
		return color.RGBA{}, 0
	}
	c := p.Base
	inside := func(spots [][3]float64) bool {
		for k, s := range spots {
			du, dv := (u-s[0])/(s[2]*0.25), (v-s[1]*w)/(s[2]*half*1.4)
			// Wavy edges so patches read as markings, not circles.
			ang := math.Atan2(dv, du)
			edge := 1 + 0.18*math.Sin(ang*3+float64(k)*1.7) + 0.1*math.Sin(ang*5+float64(k))
			if du*du+dv*dv < edge*edge {
				return true
			}
		}
		return false
	}
	if inside(p.Spots) {
		c = p.Spot
	}
	if inside(p.Spots2) {
		c = p.Spot2
	}
	// Eyes.
	for _, s := range []float64{1, -1} {
		if math.Hypot((u-0.93)/0.012, (v-s*w*0.7)/0.012) < 1 {
			c = color.RGBA{0x10, 0x10, 0x10, 0xff}
		}
	}
	// Rounded shading: darker toward the edge, sheen along the spine.
	edge := math.Abs(v) / math.Max(w, 1e-6)
	shade := 1.0 - 0.32*math.Pow(edge, 2.5)
	if p.Metallic {
		shade += 0.25 * math.Exp(-math.Pow(v/(w*0.35+1e-6), 2))
	} else {
		shade += 0.08 * math.Exp(-math.Pow(v/(w*0.3+1e-6), 2))
	}
	return scaleRGB(c, shade), 1
}

func scaleRGB(c color.RGBA, k float64) color.RGBA {
	f := func(x uint8) uint8 { return uint8(math.Max(0, math.Min(255, float64(x)*k))) }
	return color.RGBA{f(c.R), f(c.G), f(c.B), c.A}
}

// DrawKoi composites one koi into img. (cx, cy) is the body center, length
// is in image pixels, heading in radians (0 = facing +x).
func DrawKoi(img *image.RGBA, p KoiPattern, cx, cy, length, heading, wag float64) {
	drawKoi(img, p, cx, cy, length, heading, wag, false)
}

// DrawKoiAlpha draws onto a transparent image, keeping coverage as alpha so
// the sprite can be layered over water by the terminal.
func DrawKoiAlpha(img *image.RGBA, p KoiPattern, cx, cy, length, heading, wag float64) {
	drawKoi(img, p, cx, cy, length, heading, wag, true)
}

func drawKoi(img *image.RGBA, p KoiPattern, cx, cy, length, heading, wag float64, keepAlpha bool) {
	b := img.Bounds()
	r := length * 0.62
	cosH, sinH := math.Cos(heading), math.Sin(heading)
	const ss = 3 // supersampling per axis
	for y := int(cy - r); y <= int(cy+r); y++ {
		for x := int(cx - r); x <= int(cx+r); x++ {
			if !(image.Point{x, y}).In(b) {
				continue
			}
			var rs, gs, bs, as float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss - cx
					py := float64(y) + (float64(sy)+0.5)/ss - cy
					// Into koi space.
					along := (px*cosH + py*sinH) / length
					side := (-px*sinH + py*cosH) / length
					c, a := koiSample(p, along+0.5, side, wag)
					rs += float64(c.R) * a
					gs += float64(c.G) * a
					bs += float64(c.B) * a
					as += a
				}
			}
			if as == 0 {
				continue
			}
			n := float64(ss * ss)
			alpha := as / n
			src := color.RGBA{uint8(rs / as), uint8(gs / as), uint8(bs / as), 0xff}
			if keepAlpha {
				// Premultiplied, as image.RGBA expects.
				img.SetRGBA(x, y, color.RGBA{uint8(float64(src.R) * alpha), uint8(float64(src.G) * alpha), uint8(float64(src.B) * alpha), uint8(alpha * 255)})
				continue
			}
			dst := img.RGBAAt(x, y)
			img.SetRGBA(x, y, color.RGBA{
				uint8(float64(src.R)*alpha + float64(dst.R)*(1-alpha)),
				uint8(float64(src.G)*alpha + float64(dst.G)*(1-alpha)),
				uint8(float64(src.B)*alpha + float64(dst.B)*(1-alpha)), 0xff})
		}
	}
}

// KoiPondImage paints water plus the koi at time t (seconds).
func KoiPondImage(w, h int, n int, t float64, length float64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Deep water: soft vertical gradient plus slow, low-contrast light.
			k := float64(y) / float64(h)
			fx, fy := float64(x)/float64(w), float64(y)/float64(h)
			s := 0.5 + 0.25*math.Sin(fx*5.1+t*0.31+math.Sin(fy*3.7+t*0.23)*1.3) + 0.25*math.Sin(fy*4.3-t*0.27+math.Sin(fx*2.9)*1.1)
			img.SetRGBA(x, y, color.RGBA{uint8(9 + 5*s), uint8(38 + 9*s - 10*k), uint8(54 + 12*s - 12*k), 0xff})
		}
	}
	// Each koi keeps to its own horizontal lane so bodies never stack.
	lanes := max(1, n)
	laneH := float64(h) / float64(lanes)
	for i := 0; i < n; i++ {
		speed := 0.18 + koiHash(i+11)*0.12
		phase := koiHash(i+29) * math.Pi * 2
		u := t*speed + phase
		margin := length * 0.6
		cx := margin + (math.Sin(u)*0.5+0.5)*(float64(w)-2*margin)
		drift := laneH * 0.18
		cy := laneH*(float64(i)+0.5) + math.Sin(u*0.9+phase)*drift
		vx := math.Cos(u) * (float64(w) - 2*margin) * 0.5 * speed
		vy := math.Cos(u*0.9+phase) * 0.9 * drift * speed
		heading := math.Atan2(vy, vx)
		wag := math.Sin(t*6+phase*3) * (0.6 + 0.4*math.Min(1, math.Hypot(vx, vy)/20))
		DrawKoi(img, KoiPatterns[i%len(KoiPatterns)], cx, cy, length, heading, wag)
	}
	return img
}
