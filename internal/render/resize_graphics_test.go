package render

import (
	"bytes"
	"image"
	"testing"

	"github.com/kazuph/ikesu/internal/kitty"
)

func TestResizeAndRepaintDoNotClearDisplayedImages(t *testing.T) {
	var encoder Encoder
	frame := func(w, h int) *Frame {
		f := NewFrame(w, h)
		f.Graphics = []Graphic{{Pane: 1, Placement: kitty.Placement{Image: kitty.Image{ID: 42, Format: 24, Width: 2, Height: 1, Data: []byte{255, 0, 0, 0, 255, 0}}, X: 6, Y: 4, Cols: 4, Rows: 2}}}
		return f
	}
	if !bytes.Contains(encoder.Frame(frame(160, 45), 0, 0, false), []byte(clearAll)) {
		t.Fatal("initial frame must clear the previous terminal contents")
	}
	for _, size := range [][2]int{{140, 40}, {180, 50}} {
		output := encoder.Frame(frame(size[0], size[1]), 0, 0, false)
		for _, forbidden := range []string{clearAll, "a=t,", "a=d,"} {
			if bytes.Contains(output, []byte(forbidden)) {
				t.Fatalf("resize cleared or reuploaded a retained image (%q)", forbidden)
			}
		}
	}
	encoder.Reset()
	if bytes.Contains(encoder.Frame(frame(180, 50), 0, 0, false), []byte(clearAll)) {
		t.Fatal("repainting an existing terminal must not erase displayed images")
	}
}

func TestViewportRetainsVisibleSourceAndVirtualPlaceholders(t *testing.T) {
	f := NewFrame(20, 10)
	f.GraphicsNamespace = "remote"
	f.Graphics = []Graphic{
		{Pane: 1, Placement: kitty.Placement{Image: kitty.Image{ID: 1, Width: 8, Height: 4}, X: 6, Y: 4, Cols: 8, Rows: 4}, Source: image.Rect(2, 1, 8, 4)},
		{Pane: 1, Placement: kitty.Placement{Image: kitty.Image{ID: 2}, X: 18, Y: 8, Cols: 2, Rows: 2}},
		{Pane: 1, Placement: kitty.Placement{Image: kitty.Image{ID: 3}, Virtual: true, Cols: 4, Rows: 2}},
	}
	f.Placeholders = []Placeholder{{X: 7, Y: 4, ImageID: 3}, {X: 19, Y: 9, ImageID: 3}}
	f.Set(7, 4, "x", f.Cell(7, 4).Style)
	clipped := f.Viewport(10, 6)
	if clipped.GraphicsNamespace != "remote" || len(clipped.Graphics) != 2 || len(clipped.Placeholders) != 1 || clipped.Cell(7, 4).Content != "x" {
		t.Fatal("viewport lost visible surface metadata")
	}
	g := clipped.Graphics[0]
	if g.Source != image.Rect(2, 1, 5, 2) || g.Placement.Cols != 4 || g.Placement.Rows != 2 || !clipped.Graphics[1].Placement.Virtual {
		t.Fatalf("viewport altered the retained source: %+v", g)
	}
	clipped.Set(7, 4, "y", clipped.Cell(7, 4).Style)
	if f.Cell(7, 4).Content != "x" || f.Graphics[0].Placement.Cols != 8 || len(f.Placeholders) != 2 {
		t.Fatal("viewport modified the last received snapshot")
	}
}
