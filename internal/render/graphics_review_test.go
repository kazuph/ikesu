package render

import (
	"bytes"
	"image"
	"testing"

	"github.com/kazuph/goterrace/internal/kitty"
)

func TestFullOverlayRestoresWithoutImageUpload(t *testing.T) {
	frame := func() *Frame {
		f := NewFrame(20, 10)
		f.Graphics = []Graphic{{Pane: 1, Placement: kitty.Placement{Image: kitty.Image{ID: 7, Format: 24, Width: 1, Height: 1, Data: []byte{1, 2, 3}}, X: 2, Y: 2, Cols: 4, Rows: 2}}}
		return f
	}
	var e Encoder
	e.Frame(frame(), 0, 0, false)
	covered := frame()
	covered.coverGraphicsRects([]image.Rectangle{image.Rect(1, 1, 10, 8)})
	hidden := e.Frame(covered, 0, 0, false)
	restored := e.Frame(frame(), 0, 0, false)
	if bytes.Contains(hidden, []byte("d=I,")) || bytes.Contains(restored, []byte("a=t,")) || !bytes.Contains(restored, []byte("a=p,")) {
		t.Fatalf("full overlay discarded unchanged source: hidden=%q restored=%q", hidden, restored)
	}
	removed := e.Frame(NewFrame(20, 10), 0, 0, false)
	if !bytes.Contains(removed, []byte("d=I,")) {
		t.Fatal("removed source was retained after leaving the pane")
	}
}

func TestStandaloneModesHideWholeImageWhileEndpointKeepsUncoveredImage(t *testing.T) {
	s := testState(160, 45)
	pid := s.ActiveWorkspace().TabRef().Layout.Focused()
	snap := blankSnap()
	snap.Graphics = []kitty.Placement{{Image: kitty.Image{ID: 8, Format: 24, Width: 8, Height: 4, Data: make([]byte, 96)}, X: 6, Y: 4, Cols: 40, Rows: 20}}
	for _, mode := range []string{"menu", "dialog", "help", "settings", "navigator"} {
		t.Run(mode, func(t *testing.T) {
			in := Input{State: s, Panes: map[int]*PaneSnap{pid: snap}}
			switch mode {
			case "menu":
				in.Menu = &Menu{X: 145, Y: 10, Items: []MenuItem{{Label: "Close pane"}}}
			case "dialog":
				in.Dialog = &Dialog{Title: "Confirm"}
			case "help":
				in.Help = &HelpInput{}
			case "settings":
				in.Settings = &Settings{}
			case "navigator":
				in.Navigator = &Navigator{}
			}
			if got := Compose(in).Frame.Graphics; len(got) != 0 {
				t.Fatalf("standalone %s kept %d visible placements", mode, len(got))
			}
			if mode == "menu" {
				in.ServerUI = true
				if got := Compose(in).Frame.Graphics; len(got) != 1 {
					t.Fatalf("endpoint uncovered menu kept %d placements, want 1", len(got))
				}
			}
		})
	}
}
