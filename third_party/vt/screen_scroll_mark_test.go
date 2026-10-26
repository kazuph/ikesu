package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"reflect"
	"testing"
)

func TestScrollMarkBatchDirtyStates(t *testing.T) {
	cases := 0
	for _, width := range []int{3, 160} {
		for _, height := range []int{1, 5} {
			for _, state := range []string{"nil", "short", "wide", "extended", "alias"} {
				for _, top := range []int{0, height / 2} {
					for _, bottom := range []int{height, max(top+1, height-1)} {
						makeScreen := func() *Screen {
							s := NewScreen(width, height)
							s.cb = &Callbacks{}
							s.scrollback = NewScrollback(2)
							s.scroll = uv.Rect(0, top, width, bottom-top)
							s.cur.Y = top
							for y := 0; y < s.buf.Height(); y++ {
								screenTestRaw(s, 0, y, uv.Cell{Content: "x", Width: 1})
							}
							switch state {
							case "nil":
								s.buf.Touched = nil
							case "short":
								s.buf.Touched = make([]*uv.LineData, height/2)
							case "wide", "extended", "alias":
								s.buf.Touched = make([]*uv.LineData, height)
								shared := &uv.LineData{FirstCell: -2, LastCell: width + 3}
								for y := range s.buf.Touched {
									if state == "alias" {
										s.buf.Touched[y] = shared
									} else {
										s.buf.Touched[y] = &uv.LineData{FirstCell: -2, LastCell: width + 3}
									}
								}
								if state == "extended" {
									s.buf.Touched = append(s.buf.Touched, &uv.LineData{FirstCell: 1, LastCell: 2})
								}
							}
							return s
						}
						got, want := makeScreen(), makeScreen()
						for rep := 0; rep < 3; rep++ {
							if got.DeleteLine(1) != want.originalDeleteLine(1) || !reflect.DeepEqual(screenTestState(got.buf), screenTestState(want.buf)) || !reflect.DeepEqual(got.scrollback.Lines(), want.scrollback.Lines()) {
								t.Fatalf("width=%d height=%d state=%s top=%d bottom=%d rep=%d", width, height, state, top, bottom, rep)
							}
							cases++
						}
					}
				}
			}
		}
	}
	t.Logf("dirty bookkeeping matched original for %d states", cases)
}
