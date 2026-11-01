package vt

import (
	"image/color"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Compare byte-limited storage with the original expanded-cell representation.
func TestStyledHistoryMatchesExpandedCells(t *testing.T) {
	row := make(uv.Line, 80)
	for x := range row {
		row[x] = uv.Cell{Content: "x", Width: 1, Style: uv.Style{
			Fg: ansi.IndexedColor(x % 256), Bg: color.RGBA{R: 4, G: 9, B: 12, A: 255},
			UnderlineColor: ansi.TrueColor(0x123456), Underline: uv.UnderlineCurly,
			Attrs: uv.AttrBold | uv.AttrItalic,
		}, Link: uv.Link{URL: "https://example.test/色", Params: "id=1"}}
	}
	row[0].Content, row[0].Width = "界", 2
	row[1] = uv.Cell{}
	row[2].Content = "e\u0301"
	row[3].Content, row[3].Width = "👩‍💻", 2
	row[4] = uv.Cell{}
	const retained = 3
	charge := scrollbackRowBytes + len(row)*scrollbackCellBytes
	packed := NewScrollback(1)
	packed.SetMaxBytes(retained * charge)
	expanded := NewScrollback(retained)
	for n := 0; n < retained*3; n++ {
		row[5].Content = string(rune('a' + n))
		packed.Push(row)
		expanded.Push(row)
		if packed.Len() != expanded.Len() || packed.Bytes() != packed.Len()*charge {
			t.Fatalf("push %d: retained=%d/%d bytes=%d", n, packed.Len(), expanded.Len(), packed.Bytes())
		}
		for y := 0; y < packed.Len(); y++ {
			want := expanded.Line(y)
			if got := packed.Line(y); !reflect.DeepEqual(got, want) {
				t.Fatalf("push %d row %d differs", n, y)
			}
			if got := packed.lineInto(y, nil); !reflect.DeepEqual(got, want) {
				t.Fatalf("push %d row %d: lineInto differs", n, y)
			}
			for x := range want {
				if got := packed.CellAt(x, y); got == nil || !reflect.DeepEqual(*got, want[x]) {
					t.Fatalf("push %d row %d cell %d differs", n, y, x)
				}
			}
		}
	}
	packed.SetMaxBytes(retained * charge)
	for packed.Len() > 0 {
		if got, want := packed.popLast(), expanded.popLast(); !reflect.DeepEqual(got, want) {
			t.Fatal("restored row differs")
		}
	}
}
