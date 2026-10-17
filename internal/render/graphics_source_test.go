package render

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"testing"

	"github.com/kazuph/goterrace/internal/kitty"
)

func croppedSourcePlacement(t *testing.T) kitty.Placement {
	t.Helper()
	var store kitty.Store
	command := "Ga=T,f=24,s=8,v=4,i=7,c=8,r=4,x=2,y=1,w=4,h=2,X=3,Y=5;" + base64.StdEncoding.EncodeToString(make([]byte, 96))
	reply, _, _, _ := store.Handle([]byte(command), 2, 2, 8, 16, false)
	if len(store.Placements) != 1 {
		t.Fatal("crop input was rejected", reply)
	}
	encoded, err := json.Marshal(store.Placements[0])
	if err != nil {
		t.Fatal(err)
	}
	var placement kitty.Placement
	if err := json.Unmarshal(encoded, &placement); err != nil {
		t.Fatal(err)
	}
	return placement
}

func TestProducerSourceCropAndOffsetsSurviveSnapshotAndPaneEdges(t *testing.T) {
	for _, tc := range []struct {
		name             string
		x, y             int
		source           image.Rectangle
		offsetX, offsetY int
	}{
		{"inside", 2, 2, image.Rect(2, 1, 6, 3), 3, 5},
		{"left", -4, 2, image.Rect(4, 1, 6, 3), 0, 5},
		{"right", 16, 2, image.Rect(2, 1, 4, 3), 3, 5},
		{"top", 2, -2, image.Rect(2, 2, 6, 3), 3, 0},
		{"bottom", 2, 8, image.Rect(2, 1, 6, 2), 3, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := croppedSourcePlacement(t)
			p.X, p.Y = tc.x, tc.y
			f := NewFrame(20, 10)
			paneGraphics(f, 1, &PaneSnap{Graphics: []kitty.Placement{p}}, 0, 0, 20, 10)
			if len(f.Graphics) != 1 {
				t.Fatalf("visible placements=%d", len(f.Graphics))
			}
			g := f.Graphics[0]
			if g.Source != tc.source || g.Placement.OffsetX != tc.offsetX || g.Placement.OffsetY != tc.offsetY {
				t.Fatalf("source/offset=%+v, want %v %d,%d", g, tc.source, tc.offsetX, tc.offsetY)
			}
			var e Encoder
			output := e.Frame(f, 0, 0, false)
			crop := fmt.Sprintf(",x=%d,y=%d,w=%d,h=%d", tc.source.Min.X, tc.source.Min.Y, tc.source.Dx(), tc.source.Dy())
			if !bytes.Contains(output, []byte(crop)) {
				t.Fatalf("missing source pixels %s: %q", crop, output)
			}
			for key, value := range map[string]int{"X": tc.offsetX, "Y": tc.offsetY} {
				if got := bytes.Contains(output, []byte(","+key+"=")); got != (value != 0) || value != 0 && !bytes.Contains(output, []byte(fmt.Sprintf(",%s=%d", key, value))) {
					t.Fatalf("wrong leading pixel offset %s=%d: %q", key, value, output)
				}
			}
		})
	}
}

func TestProducerSourceCropAndOffsetsSurviveOverlayRoundTrip(t *testing.T) {
	frame := func() *Frame {
		f := NewFrame(20, 10)
		paneGraphics(f, 1, &PaneSnap{Graphics: []kitty.Placement{croppedSourcePlacement(t)}}, 0, 0, 20, 10)
		return f
	}
	var e Encoder
	e.Frame(frame(), 0, 0, false)
	f := frame()
	f.coverGraphicsRects([]image.Rectangle{image.Rect(6, 3, 8, 5)})
	want := []image.Rectangle{image.Rect(2, 1, 6, 2), image.Rect(2, 2, 6, 3), image.Rect(2, 1, 4, 2), image.Rect(5, 1, 6, 2)}
	if len(f.Graphics) != len(want) {
		t.Fatalf("remaining pieces=%d want %d", len(f.Graphics), len(want))
	}
	for i, g := range f.Graphics {
		if g.Source != want[i] {
			t.Fatalf("piece %d source=%v want %v", i, g.Source, want[i])
		}
		if (g.Placement.OffsetX == 3) != (g.Placement.X == 2) || (g.Placement.OffsetY == 5) != (g.Placement.Y == 2) {
			t.Fatalf("piece %d retained a leading offset away from its original edge: %+v", i, g)
		}
	}
	hidden, restored := e.Frame(f, 0, 0, false), e.Frame(frame(), 0, 0, false)
	if bytes.Contains(hidden, []byte("a=t,")) || bytes.Contains(restored, []byte("a=t,")) || !bytes.Contains(restored, []byte(",x=2,y=1,w=4,h=2,X=3,Y=5")) {
		t.Fatalf("overlay changed original crop or reuploaded source: hidden=%q restored=%q", hidden, restored)
	}
}
