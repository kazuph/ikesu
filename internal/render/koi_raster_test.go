package render

// SPIKE: KOI_PREVIEW=<dir> writes a side-by-side of the three output methods
// at real cell geometry (8x16 px cells), so they can be compared fairly.

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

const simCellW, simCellH = 8, 16

func rgbDist(a, b color.RGBA) float64 {
	dr, dg, db := float64(a.R)-float64(b.R), float64(a.G)-float64(b.G), float64(a.B)-float64(b.B)
	return dr*dr + dg*dg + db*db
}

// TwoColorCell picks two representative colors for a block of samples and
// assigns every sample to the nearer one (what an octant/half-block cell can
// show: one glyph shape, one fg and one bg).
func TwoColorCell(px []color.RGBA) (fg, bg color.RGBA, mask []bool) {
	a, b := 0, 0
	best := -1.0
	for i := range px {
		for j := i + 1; j < len(px); j++ {
			if d := rgbDist(px[i], px[j]); d > best {
				best, a, b = d, i, j
			}
		}
	}
	ca, cb := px[a], px[b]
	mask = make([]bool, len(px))
	var sa, sb [3]float64
	var na, nb float64
	for i, c := range px {
		if rgbDist(c, ca) <= rgbDist(c, cb) {
			mask[i] = true
			sa[0], sa[1], sa[2], na = sa[0]+float64(c.R), sa[1]+float64(c.G), sa[2]+float64(c.B), na+1
		} else {
			sb[0], sb[1], sb[2], nb = sb[0]+float64(c.R), sb[1]+float64(c.G), sb[2]+float64(c.B), nb+1
		}
	}
	avg := func(s [3]float64, n float64) color.RGBA {
		if n == 0 {
			return color.RGBA{0, 0, 0, 0xff}
		}
		return color.RGBA{uint8(s[0] / n), uint8(s[1] / n), uint8(s[2] / n), 0xff}
	}
	return avg(sa, na), avg(sb, nb), mask
}

// cellSim renders a pond sampled at sx x sy dots per cell and shows each cell
// the way a terminal draws block glyphs.
func cellSim(cols, rows, sx, sy int, twoColor bool, t, lengthCells float64) *image.RGBA {
	src := KoiPondImage(cols*sx, rows*sy, 4, t, lengthCells*float64(sx))
	out := image.NewRGBA(image.Rect(0, 0, cols*simCellW, rows*simCellH))
	dw, dh := simCellW/sx, simCellH/sy
	for cy := 0; cy < rows; cy++ {
		for cx := 0; cx < cols; cx++ {
			var px []color.RGBA
			for y := 0; y < sy; y++ {
				for x := 0; x < sx; x++ {
					px = append(px, src.RGBAAt(cx*sx+x, cy*sy+y))
				}
			}
			if twoColor {
				fg, bg, mask := TwoColorCell(px)
				for i := range px {
					if mask[i] {
						px[i] = fg
					} else {
						px[i] = bg
					}
				}
			}
			for i, c := range px {
				x0, y0 := cx*simCellW+(i%sx)*dw, cy*simCellH+(i/sx)*dh
				for y := 0; y < dh; y++ {
					for x := 0; x < dw; x++ {
						out.SetRGBA(x0+x, y0+y, c)
					}
				}
			}
		}
	}
	return out
}

func TestKoiResolutionPreview(t *testing.T) {
	dir := os.Getenv("KOI_PREVIEW")
	if dir == "" {
		t.Skip("set KOI_PREVIEW=<dir>")
	}
	const cols, rows = 36, 10
	const lengthCells = 12.0
	for _, tm := range []float64{1.0, 4.0} {
		methods := []struct {
			name string
			img  *image.RGBA
		}{
			{"halfblock", cellSim(cols, rows, 1, 2, false, tm, lengthCells)},
			{"octant", cellSim(cols, rows, 2, 4, true, tm, lengthCells)},
			{"image", KoiPondImage(cols*simCellW, rows*simCellH, 4, tm, lengthCells*simCellW)},
		}
		for _, m := range methods {
			// Upscale 2x for review.
			b := m.img.Bounds()
			big := image.NewRGBA(image.Rect(0, 0, b.Dx()*2, b.Dy()*2))
			for y := 0; y < b.Dy()*2; y++ {
				for x := 0; x < b.Dx()*2; x++ {
					big.SetRGBA(x, y, m.img.RGBAAt(x/2, y/2))
				}
			}
			f, err := os.Create(filepath.Join(dir, "res-"+m.name+"-t"+string(rune('0'+int(tm)))+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, big); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}
