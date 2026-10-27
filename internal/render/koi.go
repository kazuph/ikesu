package render

// SPIKE (feat/koi-branding): feasibility prototype for swimming koi in the
// sidebars. Not production code; enabled only through Input.Koi.

import (
	"image/color"
	"math"

	uv "github.com/charmbracelet/ultraviolet"
)

// koiVariety maps sprite letters to colors: a = base, b = pattern, e = eye,
// f = fins/tail.
type koiVariety struct{ a, b, e, f color.Color }

var (
	koiWhite = color.RGBA{0xf4, 0xf1, 0xea, 0xff}
	koiRed   = color.RGBA{0xe0, 0x3a, 0x2a, 0xff}
	koiBlack = color.RGBA{0x1a, 0x1a, 0x1a, 0xff}
	koiGold  = color.RGBA{0xf2, 0xb6, 0x3c, 0xff}
	koiFin   = color.RGBA{0xd8, 0xcf, 0xc0, 0xff}
	waterBG  = color.RGBA{0x0d, 0x2b, 0x3a, 0xff}
	waterHi  = color.RGBA{0x16, 0x3d, 0x52, 0xff}
)

var koiVarieties = []koiVariety{
	{koiWhite, koiRed, koiBlack, koiFin},   // kohaku
	{koiWhite, koiBlack, koiBlack, koiFin}, // shiro bekko-ish
	{koiGold, koiGold, koiBlack, koiGold},  // ogon
	{koiBlack, koiRed, koiWhite, koiFin},   // showa-ish
	{koiWhite, koiWhite, koiBlack, koiFin}, // tancho (red cap painted below)
}

// Facing right, 10x4 pixels (2 cells tall with half blocks).
var koiSprite = []string{
	"....f.....",
	"f.aabbaab.",
	".fbbaabbae",
	"f...f.....",
}

// Mini koi for a single text row: 6x2 pixels.
var koiMini = []string{
	"f.abba",
	".fbaae",
}

type pixbuf struct {
	w, h int
	px   []color.Color
}

func newPixbuf(w, h int, bg color.Color) *pixbuf {
	p := &pixbuf{w: w, h: h, px: make([]color.Color, w*h)}
	for i := range p.px {
		p.px[i] = bg
	}
	return p
}

func (p *pixbuf) set(x, y int, c color.Color) {
	if x >= 0 && y >= 0 && x < p.w && y < p.h && c != nil {
		p.px[y*p.w+x] = c
	}
}

func (p *pixbuf) sprite(sp []string, x, y int, v koiVariety, flip bool, tancho bool) {
	for r, line := range sp {
		for c := 0; c < len(line); c++ {
			col := c
			if flip {
				col = len(line) - 1 - c
			}
			var k color.Color
			switch line[c] {
			case 'a':
				k = v.a
			case 'b':
				k = v.b
			case 'e':
				k = v.e
			case 'f':
				k = v.f
			}
			if tancho && line[c] != '.' && c >= len(line)-3 && c < len(line)-1 && r <= 1 {
				k = koiRed
			}
			p.set(x+col, y+r, k)
		}
	}
}

// blit writes the pixel buffer into the frame as half-block cells.
func (p *pixbuf) blit(f *Frame, x0, y0 int) {
	for cy := 0; cy*2 < p.h; cy++ {
		for cx := 0; cx < p.w; cx++ {
			top := p.px[cy*2*p.w+cx]
			bot := top
			if cy*2+1 < p.h {
				bot = p.px[(cy*2+1)*p.w+cx]
			}
			if sameColor(top, bot) {
				f.Set(x0+cx, y0+cy, " ", uv.Style{Bg: top})
			} else {
				f.Set(x0+cx, y0+cy, "▀", uv.Style{Fg: top, Bg: bot})
			}
		}
	}
}

func sameColor(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// KoiCount limits the visible fish so dozens of agents never crowd the pond:
// linear up to 4, then square-root growth, then a hard cap by pond area.
func KoiCount(agents, pondW, pondH int) int {
	if agents <= 0 || pondW < 12 || pondH < 3 {
		return 0
	}
	n := agents
	if n > 4 {
		n = 4 + int(math.Ceil(math.Sqrt(float64(n-4))))
	}
	// One fish per ~70 cells of water keeps clear space around each koi.
	areaCap := max(1, pondW*pondH/70)
	return min(n, areaCap, 12)
}

func koiHash(i int) float64 {
	x := math.Sin(float64(i)*12.9898+78.233) * 43758.5453
	return x - math.Floor(x)
}

// drawKoiPond fills the rectangle with water and n swimming koi. t is the
// animation clock in 128ms steps.
func drawKoiPond(f *Frame, x, y, w, h, n, t int) {
	if w <= 0 || h <= 0 {
		return
	}
	pw, ph := w, h*2
	p := newPixbuf(pw, ph, waterBG)
	// Slow drifting ripples.
	for i := 0; i < pw*ph/40; i++ {
		rx := int(koiHash(i*7+1)*float64(pw)+float64(t)*0.15*(0.5+koiHash(i))) % pw
		ry := int(koiHash(i*3+2) * float64(ph))
		p.set(rx, ry, waterHi)
	}
	sw, sh := len(koiSprite[0]), len(koiSprite)
	for i := 0; i < n; i++ {
		speed := 0.025 + koiHash(i+11)*0.03
		phase := koiHash(i+29) * math.Pi * 2
		span := float64(max(1, pw-sw))
		u := float64(t)*speed + phase
		fx := (math.Sin(u)*0.5 + 0.5) * span
		dir := math.Cos(u) // derivative sign gives heading
		vspan := float64(max(0, ph-sh))
		fy := (math.Sin(u*0.37+phase*2)*0.5 + 0.5) * vspan
		v := koiVarieties[i%len(koiVarieties)]
		p.sprite(koiSprite, int(fx), int(fy), v, dir < 0, i%len(koiVarieties) == 4)
	}
	p.blit(f, x, y)
}

// drawMiniKoi swims a one-row koi through the blank tail of a text row,
// keeping the row's own background.
func drawMiniKoi(f *Frame, x, y, w, seed, t int) {
	sw := len(koiMini[0])
	if w < sw+2 {
		return
	}
	var bg color.Color = waterBG
	if c := f.Cell(x, y); c != nil && c.Style.Bg != nil {
		bg = c.Style.Bg
	}
	p := newPixbuf(w, 2, bg)
	u := float64(t)*(0.04+koiHash(seed+5)*0.03) + koiHash(seed+17)*math.Pi*2
	fx := (math.Sin(u)*0.5 + 0.5) * float64(w-sw)
	v := koiVarieties[seed%len(koiVarieties)]
	p.sprite(koiMini, int(fx), 0, v, math.Cos(u) < 0, false)
	// Only overwrite cells the fish actually occupies.
	for cx := 0; cx < w; cx++ {
		top, bot := p.px[cx], p.px[w+cx]
		if sameColor(top, bg) && sameColor(bot, bg) {
			continue
		}
		f.Set(x+cx, y, "▀", uv.Style{Fg: top, Bg: bot})
	}
}

// blankRun returns the start and width of the trailing run of blank cells in
// row y between x0 and x1 (exclusive), leaving one cell of padding.
func blankRun(f *Frame, y, x0, x1 int) (int, int) {
	end := x1
	start := end
	for xx := end - 1; xx >= x0; xx-- {
		c := f.Cell(xx, y)
		if c == nil || (c.Content != " " && c.Content != "") {
			break
		}
		start = xx
	}
	start++
	return start, max(0, end-start)
}
