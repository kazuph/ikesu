package uv

import (
	"image/color"
	"reflect"
	"testing"
)

func originalFillLines(b *Buffer, y, n int, c *Cell) {
	if c != nil && c.Width > 1 {
		for i := y; i < y+n; i++ {
			for x := range b.Lines[i] {
				b.SetCell(x, i, c)
			}
		}
		return
	}
	fill := EmptyCell
	if c != nil {
		fill = *c
	}
	for i := y; i < y+n; i++ {
		for x := range b.Lines[i] {
			b.Lines[i][x] = fill
		}
	}
}

func TestCopyFillMatchesOriginal(t *testing.T) {
	cases := 0
	fills := []*Cell{nil, {}, {Content: "x", Width: 1, Style: Style{Attrs: AttrBold, Fg: color.RGBA{R: 17, A: 255}}, Link: Link{URL: "https://example.com", Params: "id=test"}}, {Content: "世", Width: 2}, {Content: "a", Width: 0}}
	for _, w := range []int{0, 1, 3, 45, 160} {
		for _, h := range []int{0, 1, 5} {
			for y := 0; y <= h; y++ {
				for n := 0; n <= h-y; n++ {
					for _, fill := range fills {
						got, want := patternedBuffer(w, h), patternedBuffer(w, h)
						fillLines(got, y, n, fill)
						originalFillLines(want, y, n, fill)
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("w=%d h=%d y=%d n=%d fill=%+v", w, h, y, n, fill)
						}
						cases++
					}
				}
			}
		}
	}
	for _, w := range []int{1, 3, 45, 160} {
		for _, h := range []int{1, 5} {
			got, want := patternedBuffer(w, h), patternedBuffer(w, h)
			fillLines(got, 0, h, &got.Lines[0][0])
			originalFillLines(want, 0, h, &want.Lines[0][0])
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("alias w=%d h=%d", w, h)
			}
			cases++
		}
	}
	t.Logf("exact whole-buffer equality in %d cases", cases)
}
