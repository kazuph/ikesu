package render

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

func TestCopyWideTrailingCursorClearsWithoutShiftingText(t *testing.T) {
	before, after := NewFrame(30, 2), NewFrame(30, 2)
	before.Text(0, 0, "日本語 alpha", uv.Style{})
	after.Text(0, 0, "日本語 alpha", uv.Style{})
	drawCopyCursor(before, 3, 0, false)
	drawCopyCursor(after, 0, 1, false)
	enc := &Encoder{}
	emu := vt.NewEmulator(30, 2)
	defer emu.Close()
	for _, frame := range []*Frame{before, after} {
		if _, err := emu.Write(enc.Frame(frame, 0, 0, false)); err != nil {
			t.Fatal(err)
		}
	}
	for x := 0; x < after.W; x++ {
		want, got := after.Cell(x, 0), emu.CellAt(x, 0)
		if got.Content != want.Content || got.Width != want.Width || !got.Style.Equal(&want.Style) {
			t.Fatalf("wide cursor left stale content at %d: got %+v, want %+v", x, got, want)
		}
	}
}
