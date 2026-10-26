//go:build flatcells

package vt

import (
	"image/color"
	"math/rand"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestFlatBufferOperationsMatchExistingRenderBuffer(t *testing.T) {
	legacy := uv.NewRenderBuffer(16, 7)
	flat := newFlatScreenBuffer(16, 7)
	cells := []uv.Cell{
		{}, uv.EmptyCell,
		{Content: "x", Width: 1}, {Content: "界", Width: 2}, {Content: "e\u0301", Width: 1},
		{Content: "👩‍💻", Width: 2, Style: uv.Style{Fg: color.RGBA{R: 19, G: 83, B: 151, A: 255}, Bg: color.RGBA{B: 73, A: 255}, UnderlineColor: color.RGBA{R: 41, A: 255}, Underline: uv.UnderlineCurly, Attrs: uv.AttrItalic}, Link: uv.Link{URL: "https://example.test/界", Params: "id=exact"}},
	}
	rng := rand.New(rand.NewSource(1))
	for step := 0; step < 200; step++ {
		w, h := legacy.Width(), legacy.Height()
		if w == 0 || h == 0 {
			legacy.Resize(16, 7)
			flat.Resize(16, 7)
			w, h = 16, 7
		}
		x, y := rng.Intn(w), rng.Intn(h)
		area := uv.Rect(0, 0, w, h)
		if rng.Intn(2) == 0 {
			area = uv.Rect(x, y, w-x, h-y)
		}
		cell := &cells[rng.Intn(len(cells))]
		n, op := 1+rng.Intn(max(w, h)), rng.Intn(9)
		switch op {
		case 0:
			legacy.SetCell(x, y, cell)
			flat.SetCell(x, y, cell)
		case 1:
			legacy.InsertLineArea(y, n, cell, area)
			flat.InsertLineArea(y, n, cell, area)
		case 2:
			legacy.DeleteLineArea(y, n, cell, area)
			flat.DeleteLineArea(y, n, cell, area)
		case 3:
			legacy.InsertCellArea(x, y, n, cell, area)
			flat.InsertCellArea(x, y, n, cell, area)
		case 4:
			legacy.DeleteCellArea(x, y, n, cell, area)
			flat.DeleteCellArea(x, y, n, cell, area)
		case 5:
			legacy.FillArea(cell, area)
			flat.FillArea(cell, area)
		case 6:
			legacy.ClearArea(area)
			flat.ClearArea(area)
		case 7:
			newW, newH := rng.Intn(17), rng.Intn(8)
			legacy.Resize(newW, newH)
			flat.Resize(newW, newH)
		case 8:
			legacy.Clear()
			flat.Clear()
		}
		if legacy.Bounds() != flat.Bounds() || !reflect.DeepEqual(legacy.Touched, flat.Touched) {
			t.Fatalf("step=%d op=%d bounds or touched mismatch: %v/%v %#v/%#v", step, op, legacy.Bounds(), flat.Bounds(), legacy.Touched, flat.Touched)
		}
		for row := 0; row < legacy.Height(); row++ {
			if got, want := flat.Line(row), legacy.Line(row); !reflect.DeepEqual(got, want) {
				t.Fatalf("step=%d op=%d x=%d y=%d n=%d area=%v row=%d\ngot=%#v\nwant=%#v", step, op, x, y, n, area, row, got, want)
			}
		}
		if legacy.String() != flat.String() || legacy.Render() != flat.Render() {
			t.Fatalf("step=%d op=%d text or ANSI changed", step, op)
		}
		if step%2 == 0 {
			legacy.Touched, flat.Touched = nil, nil
		}
	}
}

func TestFlatBufferDecodedRowsStayImmutableAndTablesRelease(t *testing.T) {
	b := newFlatScreenBuffer(16, 7)
	cell := uv.Cell{Content: "e\u0301", Width: 2, Style: uv.Style{Fg: flatSliceColor{3, 5, 7}, Underline: uv.UnderlineDouble}, Link: uv.Link{URL: "https://example.test/retained"}}
	b.SetCell(0, 0, &cell)
	saved := b.Line(0)
	b.DeleteLineArea(0, 1, nil, b.Bounds())
	b.Resize(37, 13)
	b.SetCell(0, 0, &uv.Cell{Content: "new\u0301", Width: 1})
	b.Clear()
	if !reflect.DeepEqual(saved[0], cell) {
		t.Fatal("later mutation changed decoded row", saved[0], cell)
	}
	if b.tables.text.live != 0 || b.tables.styles.live != 0 || b.tables.links.live != 0 || b.tables.live != 0 {
		t.Fatal("clear retained table entries")
	}
}

func TestFlatBufferWideDeletePreservesLegacyFixupOrder(t *testing.T) {
	for wide := 0; wide < 8; wide++ {
		for at := 0; at < 8; at++ {
			for n := 1; n <= 8; n++ {
				legacy := uv.NewRenderBuffer(8, 1)
				flat := newFlatScreenBuffer(8, 1)
				for x := 0; x < 8; x++ {
					c := uv.Cell{Content: string(rune('a' + x)), Width: 1}
					legacy.SetCell(x, 0, &c)
					flat.SetCell(x, 0, &c)
				}
				c := uv.Cell{Content: "界", Width: 2, Style: uv.Style{Fg: color.RGBA{R: 19, G: 83, B: 151, A: 255}}, Link: uv.Link{URL: "https://example.test/wide"}}
				legacy.SetCell(wide, 0, &c)
				flat.SetCell(wide, 0, &c)
				legacy.DeleteCellArea(at, 0, n, nil, legacy.Bounds())
				flat.DeleteCellArea(at, 0, n, nil, flat.Bounds())
				if got, want := flat.Line(0), legacy.Line(0); !reflect.DeepEqual(got, want) {
					t.Fatalf("wide=%d at=%d n=%d\ngot=%#v\nwant=%#v", wide, at, n, got, want)
				}
			}
		}
	}
}
