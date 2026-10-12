package render

import (
	"bytes"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/kitty"
)

func TestVirtualImageIDIsolationAndClippedCells(t *testing.T) {
	grid := func() [][]uv.Cell {
		e := vt.NewEmulator(4, 2)
		defer e.Close()
		_, _ = e.WriteString("\x1b[38;5;42m\U0010eeee\u0305\u0305\U0010eeee\u0305\u030d\U0010eeee\u0305\u030e\U0010eeee\u0305\u0310\x1b[2;1H\U0010eeee\u030d\u0305\U0010eeee\u030d\u030d\U0010eeee\u030d\u030e\U0010eeee\u030d\u0310\x1b[0m")
		rows := make([][]uv.Cell, 2)
		for y := range rows {
			for x := 0; x < 4; x++ {
				rows[y] = append(rows[y], *e.CellAt(x, y))
			}
		}
		return rows
	}
	red, blue := []byte{255, 0, 0}, []byte{0, 0, 255}
	prototype := func(data []byte) []kitty.Placement {
		return []kitty.Placement{{Image: kitty.Image{ID: 42, Format: 24, Width: 1, Height: 1, Data: data}, Cols: 4, Rows: 2, Virtual: true}}
	}
	a := &PaneSnap{Grid: grid(), Graphics: prototype(red)}
	b := &PaneSnap{Grid: grid(), Graphics: prototype(blue)}
	var encoder Encoder
	outer := vt.NewEmulator(16, 6)
	defer outer.Close()
	var images kitty.Store
	outer.RegisterApcHandler(func(data []byte) bool {
		pos := outer.CursorPosition()
		_, _, _, handled := images.Handle(data, pos.X, pos.Y, 0, 0, false)
		return handled
	})
	frame := NewFrame(16, 6)
	frame.CopyGrid(3, 2, clipGrid(a.Grid, 2, 1))
	paneGraphics(frame, 1, a, 3, 2, 2, 1)
	frame.CopyGrid(10, 2, clipGrid(b.Grid, 2, 1))
	paneGraphics(frame, 2, b, 10, 2, 2, 1)
	encoded := encoder.Frame(frame, 0, 0, false)
	if bytes.Index(encoded, []byte("a=p,U=1")) > bytes.Index(encoded, []byte(string(kitty.PlaceholderRune))) {
		t.Fatal("virtual prototype must precede the text cells")
	}
	_, _ = outer.Write(encoded)
	ids := []uint32{kitty.ColorID(outer.CellAt(3, 2).Style.Fg), kitty.ColorID(outer.CellAt(10, 2).Style.Fg)}
	if ids[0] == ids[1] || len(images.Placements) != 2 || !bytes.Equal(images.Images[ids[0]].Data, red) || !bytes.Equal(images.Images[ids[1]].Data, blue) {
		t.Fatalf("pane-local ID collision: ids=%v prototypes=%+v", ids, images.Placements)
	}
	for _, x := range []int{3, 4, 10, 11} {
		marks := []rune(outer.CellAt(x, 2).Content)
		if len(marks) != 4 || marks[1] != '\u0305' || marks[2] != []rune{'\u0305', '\u030d'}[(x-3)%7] {
			t.Fatalf("right clipping changed source coordinates at %d: %U", x, marks)
		}
	}
	// Only the lower-right source quarter remains in this viewer. Its source
	// coordinates must remain (1,2)/(1,3), rather than becoming (0,0)/(0,1).
	a.Grid = [][]uv.Cell{a.Grid[1][2:]}
	frame = NewFrame(16, 6)
	frame.CopyGrid(3, 2, a.Grid)
	paneGraphics(frame, 1, a, 3, 2, 2, 1)
	_, _ = outer.Write(encoder.Frame(frame, 0, 0, false))
	for col := 0; col < 2; col++ {
		marks := []rune(outer.CellAt(3+col, 2).Content)
		if len(marks) != 4 || marks[1] != '\u030d' || marks[2] != []rune{'\u030e', '\u0310'}[col] {
			t.Fatalf("lower/right source clipping changed coordinates: %U", marks)
		}
	}
	if len(images.Placements) != 1 {
		t.Fatal("hidden pane's virtual image was retained")
	}
	t.Logf("same pane-local image ID 42 maps to outer IDs %v with red/blue sources; clipped lower-right cells retain source (row=1,cols=2..3)", ids)
}
