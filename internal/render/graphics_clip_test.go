package render

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/kazuph/ikesu/internal/kitty"
)

func TestGraphicsClipAtPaneEdges(t *testing.T) {
	image := kitty.Image{ID: 91, Format: 24, Width: 8, Height: 4, Data: make([]byte, 8*4*3)}
	for _, tc := range []struct {
		name                       string
		x, y, c, r, sx, sy, sw, sh int
	}{
		{"right", 8, 2, 2, 2, 0, 0, 4, 4},
		{"left", -2, 2, 2, 2, 4, 0, 4, 4},
		{"bottom", 2, 7, 4, 1, 0, 0, 8, 2},
		{"top", 2, -1, 4, 1, 0, 2, 8, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := NewFrame(20, 15)
			snap := &PaneSnap{Graphics: []kitty.Placement{{Image: image, X: tc.x, Y: tc.y, Cols: 4, Rows: 2}}}
			paneGraphics(f, 1, snap, 3, 2, 10, 8)
			if len(f.Graphics) != 1 {
				t.Fatalf("visible image region was discarded: %+v", f.Graphics)
			}
			p := f.Graphics[0].Placement
			if p.X != 3+max(0, tc.x) || p.Y != 2+max(0, tc.y) || p.Cols != tc.c || p.Rows != tc.r {
				t.Fatalf("clipped geometry: %+v", p)
			}
			var e Encoder
			output := e.Frame(f, 0, 0, false)
			for _, field := range []string{fmt.Sprintf(",w=%d", tc.sw), fmt.Sprintf(",h=%d", tc.sh)} {
				if !bytes.Contains(output, []byte(field)) {
					t.Fatalf("missing source crop %s: %q", field, output)
				}
			}
			if tc.sx > 0 && !bytes.Contains(output, []byte(fmt.Sprintf(",x=%d", tc.sx))) {
				t.Fatalf("missing source x: %q", output)
			}
			if tc.sy > 0 && !bytes.Contains(output, []byte(fmt.Sprintf(",y=%d", tc.sy))) {
				t.Fatalf("missing source y: %q", output)
			}
			if snap.Graphics[0].X != tc.x || snap.Graphics[0].Y != tc.y {
				t.Fatal("viewer mutated producer placement")
			}
		})
	}
	f := NewFrame(20, 15)
	paneGraphics(f, 1, &PaneSnap{Graphics: []kitty.Placement{{Image: image, X: 10, Cols: 4, Rows: 2}}}, 3, 2, 10, 8)
	if len(f.Graphics) != 0 {
		t.Fatal("entirely offscreen image leaked into a neighbor")
	}
}

func TestGraphicsClipScaledSourceUsesReferencePixelExtent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		x, y, c, r int
		crop       string
	}{
		{"right", 6, 2, 7, 4, ",x=0,y=0,w=4,h=4"},
		{"subpixel", 6, 2, 80, 4, ",x=0,y=0,w=1,h=4"},
		{"left", -3, 2, 7, 4, ",x=3,y=0,w=4,h=4"},
		{"top", 2, -2, 8, 7, ",x=0,y=1,w=8,h=2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := NewFrame(20, 15)
			paneGraphics(f, 1, &PaneSnap{Graphics: []kitty.Placement{{Image: kitty.Image{ID: 91, Format: 24, Width: 8, Height: 4, Data: make([]byte, 96)}, X: tc.x, Y: tc.y, Cols: tc.c, Rows: tc.r}}}, 0, 0, 10, 8)
			var e Encoder
			output := e.Frame(f, 0, 0, false)
			if !bytes.Contains(output, []byte(tc.crop)) {
				t.Fatalf("reference source region %s missing: %q", tc.crop, output)
			}
		})
	}
}
