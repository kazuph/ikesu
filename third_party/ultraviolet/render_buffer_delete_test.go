package uv

import (
	"fmt"
	"reflect"
	"testing"
)

// The reference keeps the original marking sequence, independently of the
// optimized method, so both cell contents and dirty ranges must stay identical.
func referenceRenderDelete(b *RenderBuffer, y, n int, fill *Cell, area Rectangle) {
	b.Buffer.DeleteLineArea(y, n, fill, area)
	for row := area.Min.Y; row < area.Max.Y; row++ {
		b.TouchLine(area.Min.X, row, area.Max.X-area.Min.X)
		b.TouchLine(area.Min.X, row+n, area.Max.X-area.Min.X)
	}
}

func TestRenderDeleteLineAreaMatchesOriginal(t *testing.T) {
	cases := 0
	for _, height := range []int{1, 4, 12} {
		for _, width := range []int{1, 8} {
			for top := 0; top < height; top++ {
				for bottom := top + 1; bottom <= height; bottom++ {
					for _, n := range []int{-2, 0, 1, 2, height, height + 1} {
						for _, y := range []int{-1, top, bottom - 1, bottom, height} {
							for _, state := range []string{"clean", "marked", "short", "nil", "extended"} {
								for _, fill := range []*Cell{nil, {Content: "x", Width: 1, Style: Style{Attrs: AttrBold}}} {
									makeBuffer := func() *RenderBuffer {
										b := NewRenderBuffer(width, height)
										b.Buffer = patternedBuffer(width, height)
										switch state {
										case "marked", "short", "extended":
											for row := range height {
												b.TouchLine(-1, row, width+2)
											}
										}
										switch state {
										case "short":
											b.Touched = b.Touched[:height/2]
										case "nil":
											b.Touched = nil
										case "extended":
											b.Touched = append(b.Touched, &LineData{FirstCell: -3, LastCell: width + 3})
										}
										return b
									}
									got, want := makeBuffer(), makeBuffer()
									area := Rect(0, top, width, bottom-top)
									if width > 1 {
										area.Min.X = 1
										area.Max.X = width - 1
									}
									got.DeleteLineArea(y, n, fill, area)
									referenceRenderDelete(want, y, n, fill, area)
									if !reflect.DeepEqual(got, want) {
										t.Fatal(fmt.Sprintf("height=%d width=%d top=%d bottom=%d y=%d n=%d state=%s fill=%v\ngot=%+v\nwant=%+v", height, width, top, bottom, y, n, state, fill, got, want))
									}
									cases++
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("cell contents and dirty ranges matched in %d cases", cases)
}

func TestRenderDeleteLineAreaOutsideBoundsMatchesOriginal(t *testing.T) {
	for _, area := range []Rectangle{Rect(-2, -2, 12, 9), Rect(1, 6, 4, 3), Rect(0, -5, 8, 3), Rect(2, 2, 3, 0)} {
		for _, n := range []int{-10, -1, 0, 1, 3, 10} {
			got, want := NewRenderBuffer(8, 5), NewRenderBuffer(8, 5)
			got.Touched, want.Touched = nil, nil
			// A row beyond the screen leaves cells alone but still exercises all
			// original dirty-range calls, including clipped and disjoint intervals.
			got.DeleteLineArea(5, n, nil, area)
			referenceRenderDelete(want, 5, n, nil, area)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("area=%v n=%d: changed dirty ranges", area, n)
			}
		}
	}
}
