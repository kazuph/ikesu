package render

import (
	"fmt"
	"image"
	"reflect"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/model"
)

func graphicCells(graphics []Graphic, pane int) map[image.Point]bool {
	cells := map[image.Point]bool{}
	for _, graphic := range graphics {
		if graphic.Pane != pane {
			continue
		}
		p := graphic.Placement
		for y := p.Y; y < p.Y+p.Rows; y++ {
			for x := p.X; x < p.X+p.Cols; x++ {
				cells[image.Pt(x, y)] = true
			}
		}
	}
	return cells
}

func TestEndpointPopupCropsBackgroundAndRetainsItsOwnImage(t *testing.T) {
	state := testState(160, 45)
	pid := state.FocusedPane()
	snapshot := blankSnap()
	snapshot.Graphics = []kitty.Placement{{Image: kitty.Image{ID: 8, Format: 24, Width: 8, Height: 4, Data: make([]byte, 96)}, Cols: 127, Rows: 40}}
	in := Input{ServerUI: true, State: state, Panes: map[int]*PaneSnap{pid: snapshot}}
	base := Compose(in)
	background := graphicCells(base.Frame.Graphics, pid)
	var encoder Encoder
	encoder.Frame(base.Frame, 0, 0, false)
	backgroundID := encoder.graphicsMap[graphicKey(pid, 8)]
	state.Apply(model.CmdOpenPopup{WorkspaceID: state.ActiveWorkspace().ID, Width: api.PopupSize{Cells: 20}, Height: api.PopupSize{Cells: 10}})
	popup := state.Popup.PaneID
	pane := blankSnap()
	pane.Graphics = []kitty.Placement{{Image: kitty.Image{ID: 9, Format: 24, Width: 8, Height: 4, Data: make([]byte, 96)}, Cols: 6, Rows: 3}}
	in.Panes[popup] = pane
	outer := state.PopupRect()
	covered := image.Rect(outer.X, outer.Y, outer.X+outer.W, outer.Y+outer.H)
	opened := Compose(in)
	expected := map[image.Point]bool{}
	for point := range background {
		if !point.In(covered) {
			expected[point] = true
		}
	}
	if got := graphicCells(opened.Frame.Graphics, pid); !reflect.DeepEqual(got, expected) {
		t.Fatalf("background cells=%d want=%d; popup must crop only its outer rectangle", len(got), len(expected))
	}
	if got := graphicCells(opened.Frame.Graphics, popup); len(got) != 18 {
		t.Fatalf("popup hid its own image: cells=%d want=18", len(got))
	}
	openedWire := string(encoder.Frame(opened.Frame, 0, 0, false))
	if strings.Count(openedWire, "a=t,")+strings.Count(openedWire, "a=T,") != 1 {
		t.Fatal("opening popup retransmitted unchanged background pixels", openedWire)
	}
	if strings.Contains(openedWire, fmt.Sprintf("a=d,d=I,i=%d,", backgroundID)) {
		t.Fatal("popup removed background image body", openedWire)
	}
	in.Menu = &Menu{X: outer.X + 2, Y: outer.Y + 1, Items: []MenuItem{{Label: "menu"}}}
	withMenu := Compose(in)
	r := withMenu.MenuRect
	menu := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)
	for _, graphic := range withMenu.Frame.Graphics {
		p := graphic.Placement
		if !image.Rect(p.X, p.Y, p.X+p.Cols, p.Y+p.Rows).Intersect(menu).Empty() {
			t.Fatal("background or popup image overlaps higher menu", graphic)
		}
	}
	in.Menu = nil
	state.Apply(model.CmdClosePopup{Pane: popup})
	closed := Compose(in)
	if got := graphicCells(closed.Frame.Graphics, pid); !reflect.DeepEqual(got, background) {
		t.Fatal("closing popup did not restore all original background cells")
	}
	closedWire := string(encoder.Frame(closed.Frame, 0, 0, false))
	if strings.Contains(closedWire, "a=t,") || strings.Contains(closedWire, "a=T,") || strings.Contains(closedWire, fmt.Sprintf("a=d,d=I,i=%d,", backgroundID)) {
		t.Fatal("closing popup retransmitted or deleted unchanged background pixels", closedWire)
	}
}

func TestPopupAndMenuShareTheSameAdditionalPlacementBudget(t *testing.T) {
	for _, count := range []int{16, 17} {
		frame := NewFrame(40, 20)
		for index := range count {
			frame.Graphics = append(frame.Graphics, Graphic{Pane: 1, Placement: kitty.Placement{Image: kitty.Image{ID: uint32(index + 1), Width: 100, Height: 40}, Cols: 10, Rows: 4}})
		}
		own := Graphic{Pane: 99, Placement: kitty.Placement{Image: kitty.Image{ID: 99}, X: 4, Cols: 1, Rows: 1}}
		frame.Graphics = append(frame.Graphics, own)
		frame.coverGraphicsLayers([]image.Rectangle{image.Rect(8, 0, 9, 4)}, image.Rect(4, 0, 6, 4), 99)
		want := count*3 + 1
		if count == 17 {
			want = 1
		}
		if len(frame.Graphics) != want || !reflect.DeepEqual(frame.Graphics[len(frame.Graphics)-1], own) {
			t.Fatalf("%d shared extra placements: got=%d want=%d", count*2, len(frame.Graphics), want)
		}
	}
}
