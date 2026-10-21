package render

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/kitty"
)

func TestGraphicsReuseAndReplacementOrder(t *testing.T) {
	frame := func(x int, pixels []byte) *Frame {
		f := NewFrame(40, 20)
		f.Graphics = []Graphic{
			{Pane: 1, Placement: kitty.Placement{Image: kitty.Image{ID: 41, Format: 24, Width: 1, Height: 1, Data: []byte{0, 255, 0}}, X: 2, Y: 10, Cols: 4, Rows: 2}},
			{Pane: 2, Placement: kitty.Placement{Image: kitty.Image{ID: 42, Format: 24, Width: 1, Height: 1, Data: pixels}, X: x, Y: 2, Cols: 4, Rows: 2}},
		}
		return f
	}
	var encoder Encoder
	encoder.Frame(frame(2, []byte{255, 0, 0}), 0, 0, false)
	changed := encoder.Frame(frame(2, []byte{0, 0, 255}), 0, 0, false)
	if n := bytes.Count(changed, []byte("a=t,")); n != 1 {
		t.Fatalf("one changed source must upload once, got %d: %q", n, changed)
	}
	upload, place, remove := bytes.Index(changed, []byte("a=t,")), bytes.LastIndex(changed, []byte("a=p,")), bytes.Index(changed, []byte("a=d,"))
	if upload < 0 || place <= upload || remove <= place {
		t.Fatalf("old image must be deleted after the replacement is placed: %q", changed)
	}
	moved := encoder.Frame(frame(7, []byte{0, 0, 255}), 0, 0, false)
	if bytes.Contains(moved, []byte("a=t,")) || bytes.Contains(moved, []byte("d=I,")) || !bytes.Contains(moved, []byte("a=p,")) {
		t.Fatalf("position change must reuse pixel data: %q", moved)
	}
	encoder.Reset()
	repainted := encoder.Frame(frame(7, []byte{0, 0, 255}), 0, 0, false)
	if bytes.Contains(repainted, []byte("a=t,")) {
		t.Fatalf("text repaint uploaded unchanged pixels: %q", repainted)
	}
	blank := encoder.Frame(NewFrame(40, 20), 0, 0, false)
	if bytes.Count(blank, []byte("d=I,")) != 2 {
		t.Fatalf("hidden sources must be released: %q", blank)
	}
}

func TestGraphicsReuseRemovesOnlyObsoletePlacements(t *testing.T) {
	var encoder Encoder
	var images kitty.Store
	outer := vt.NewEmulator(40, 20)
	defer outer.Close()
	outer.RegisterApcHandler(func(data []byte) bool {
		position := outer.CursorPosition()
		_, _, _, handled := images.Handle(data, position.X, position.Y, 8, 16, false)
		return handled
	})
	frame := func() *Frame {
		f := NewFrame(40, 20)
		for i := 0; i < 3; i++ {
			f.Graphics = append(f.Graphics, Graphic{Pane: 1, Placement: kitty.Placement{
				Image: kitty.Image{ID: 42, Format: 24, Width: 1, Height: 1, Data: []byte{0, 255, 0}},
				X:     i * 5, Y: 2, Cols: 4, Rows: 2, Virtual: i == 2,
			}})
		}
		return f
	}
	_, _ = outer.Write(encoder.Frame(frame(), 0, 0, false))
	if len(images.Images) != 1 || len(images.Placements) != 3 {
		t.Fatalf("initial image and placements: %+v", images)
	}
	f := frame()
	f.Graphics = f.Graphics[1:]
	encoded := encoder.Frame(f, 0, 0, false)
	_, _ = outer.Write(encoded)
	if bytes.Contains(encoded, []byte("a=t,")) || len(images.Images) != 1 || len(images.Placements) != 2 {
		t.Fatalf("removing one placement must retain the shared source and two siblings: %q %+v", encoded, images)
	}
	f = frame()
	f.Graphics = f.Graphics[:1]
	_, _ = outer.Write(encoder.Frame(f, 0, 0, false))
	if len(images.Images) != 1 || len(images.Placements) != 1 || images.Placements[0].Virtual {
		t.Fatalf("obsolete virtual prototype survived: %+v", images)
	}
}
