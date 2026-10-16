package client

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/tui"
)

func TestOutsideMouseLeavesClientDragPending(t *testing.T) {
	for _, pixel := range []bool{false, true} {
		m := &machineClient{size: tui.Winsize{Cols: 160, Rows: 45}}
		m.tabMouse = tabMouseCoordinates{pixel: pixel, width: 8, height: 16}
		drag := &tabFeedback{drop: 2, moved: true}
		m.tabFeedback = drag
		width, height := 160, 45
		if pixel {
			width, height = width*8, height*16
		}
		for _, kind := range []input.Kind{input.KindMouseClick, input.KindMouseMotion, input.KindMouseRelease} {
			for _, point := range [][2]int{{-1, 0}, {0, -1}, {width, 0}, {0, height}} {
				if err := m.input(input.Event{Kind: kind, Mouse: uv.Mouse{X: point[0], Y: point[1]}}); err != nil {
					t.Fatal(err)
				}
				if m.tabFeedback != drag || drag.drop != 2 {
					t.Fatal("outside event modified client feedback", pixel, kind, point)
				}
			}
		}
	}
}
