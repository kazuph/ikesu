package vt

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"image/color"
	"reflect"
	"testing"
)

func TestScrollRowWindowTransitionsMatchOriginal(t *testing.T) {
	for _, height := range []int{1, 3, 45} {
		for _, op := range []string{"insert", "delete-two", "resize", "partial", "background", "reset", "reflow"} {
			t.Run(fmt.Sprintf("%d-%s", height, op), func(t *testing.T) {
				got, want := NewScreen(8, height), NewScreen(8, height)
				for _, s := range []*Screen{got, want} {
					s.cb = &Callbacks{}
					s.scrollback.SetMaxBytes(4096)
					for y := 0; y < height; y++ {
						s.SetCell(0, y, &uv.Cell{Content: fmt.Sprintf("start-%d", y), Width: 1})
					}
				}
				for step := 0; step < 4*height+8; step++ {
					if step == height+1 {
						for _, s := range []*Screen{got, want} {
							switch op {
							case "insert":
								s.InsertLine(1)
							case "delete-two":
								if s == got {
									s.DeleteLine(2)
								} else {
									s.originalDeleteLine(2)
								}
							case "resize":
								s.Resize(3, height+1)
							case "partial":
								if height > 1 {
									s.scroll = uv.Rect(0, 1, s.Width(), height-1)
								}
							case "background":
								s.cur.Pen.Bg = color.RGBA{R: 1, A: 255}
							case "reset":
								s.Reset()
							case "reflow":
								s.reflow(3, height+1, nil, false)
								if len(s.scrollRows) != 0 {
									t.Fatal("reflow retained previous row headers")
								}
							}
						}
					}
					for _, s := range []*Screen{got, want} {
						s.cur.X = 0
						s.cur.Y = s.scroll.Min.Y
						s.SetCell(0, s.scroll.Max.Y-1, &uv.Cell{Content: fmt.Sprint(step), Width: 1})
					}
					got.DeleteLine(1)
					want.originalDeleteLine(1)
					if !reflect.DeepEqual(screenTestState(got.buf), screenTestState(want.buf)) || !reflect.DeepEqual(got.scrollback.Lines(), want.scrollback.Lines()) || got.scrollback.Bytes() != want.scrollback.Bytes() {
						t.Fatalf("step %d", step)
					}
				}
			})
		}
	}
}

func TestScrollRowWindowRetainedCellPointers(t *testing.T) {
	for _, height := range []int{1, 3, 45} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			got, want := NewScreen(8, height), NewScreen(8, height)

			for _, s := range []*Screen{got, want} {
				s.cb = &Callbacks{}
			}
			for step := 0; step < 4*height+8; step++ {
				got.DeleteLine(1)
				want.originalDeleteLine(1)
				// The original row selected by step%height is now the bottom row.
				for _, s := range []*Screen{got, want} {
					c := *s.CellAt(0, height-1)
					c.Content = fmt.Sprint(step)
					c.Style.Attrs = uv.AttrBold
					s.SetCell(0, height-1, &c)
				}
				if !reflect.DeepEqual(screenTestState(got.buf), screenTestState(want.buf)) || !reflect.DeepEqual(got.scrollback.Lines(), want.scrollback.Lines()) {
					t.Fatalf("step %d changed retained cell pointer behavior", step)
				}
			}
		})
	}
}
