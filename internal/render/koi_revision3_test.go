package render

import (
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/layout"
	"image/color"
	"math"
	"testing"
)

func TestKoiRev3ServerUISourcesHaveSingleOwner(t *testing.T) {
	in := koiFixture(t, 2)
	in.ServerUI = true
	in.KoiKitty = true
	in.KoiCellW = 8
	in.KoiCellH = 16
	pid := in.State.FocusedPane()
	in.Panes[pid].Graphics = []kitty.Placement{{Image: kitty.Image{ID: 99, Format: 32, Width: 1, Height: 1, Data: []byte{255, 0, 0, 255}}, X: 2, Y: 2, Cols: 2, Rows: 2}}
	out := Compose(in)
	regular, koi := withoutKoi(out.Frame)
	if len(koi) == 0 || len(regular.Graphics) == 0 {
		t.Fatal("fixture must exercise simultaneous koi and pane images")
	}
	for _, g := range regular.GraphicsSources {
		if g.Koi {
			t.Fatal("koi is also owned by ordinary image sender")
		}
	}
}

func octantCentroid(f *Frame) (float64, float64) {
	sx, sy, n := 0., 0., 0.
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			c := f.Cell(x, y)
			mask := -1
			for i, r := range octantRunes {
				if c.Content == string(r) {
					mask = i
					break
				}
			}
			if mask < 0 {
				continue
			}
			for i := 0; i < 8; i++ {
				v := c.Style.Bg
				if mask&(1<<i) != 0 {
					v = c.Style.Fg
				}
				if v != nil && color.RGBAModel.Convert(v) != waterBG {
					sx += float64(x*2 + i%2)
					sy += float64(y*4 + i/2)
					n++
				}
			}
		}
	}
	return sx / n, sy / n
}
func TestKoiRev3OctantsMoveOneDotAcrossCellBoundary(t *testing.T) {
	pose := func(x, y float64) (float64, float64) {
		f := NewFrame(40, 30)
		for i := range f.Cells {
			f.Cells[i].Style.Bg = waterBG
		}
		fish := KoiFish{Agent: KoiAgent{Seed: 0}, Rect: layout.Rect{X: int(x), Y: int(y), W: 10, H: 5}, Lane: layout.Rect{W: 40, H: 30}, X: x, Y: y}
		drawKoiOctants(f, fish)
		return octantCentroid(f)
	}
	ax, ay := pose(9.5, 10.75)
	bx, by := pose(10, 10.75)
	cx, cy := pose(9.5, 11)
	if math.Abs((bx-ax)-1) > .5 || math.Abs(by-ay) > .5 {
		t.Fatalf("horizontal subcell lost: %.3f,%.3f", bx-ax, by-ay)
	}
	if math.Abs((cy-ay)-1) > .5 || math.Abs(cx-ax) > .5 {
		t.Fatalf("vertical subcell lost: %.3f,%.3f", cx-ax, cy-ay)
	}
}
