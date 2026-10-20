package client

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestTabMousePixelFeedbackPreservesServerInputBytes(t *testing.T) {
	var mouse tabMouseCoordinates
	mouse.report(uv.CellSizeEvent{Width: 8, Height: 16}, tui.Winsize{})
	mouse.output([]byte("\x1b[?10"))
	mouse.output([]byte("16h"))
	var framer input.Framer
	raw := "\x1b[<0;213;9M"
	events := framer.Push([]byte(raw))
	if len(events) != 1 {
		t.Fatal(events)
	}
	cell := mouse.cells(events[0])
	if cell.Mouse.X != 26 || cell.Mouse.Y != 0 || string(cell.Raw) != raw {
		t.Fatal("pixel feedback did not use cells while preserving raw input", cell)
	}
	mouse.output([]byte("\x1b[?1016l"))
	cell = mouse.cells(events[0])
	if cell.Mouse.X != events[0].Mouse.X || cell.Mouse.Y != events[0].Mouse.Y {
		t.Fatal("cell mode retained pixel conversion", cell)
	}
}
