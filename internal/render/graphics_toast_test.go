package render

import (
	"image"
	"reflect"
	"testing"

	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/layout"
)

func TestEndpointPositionlessToastDoesNotDisplayOrCrop(t *testing.T) {
	state := testState(160, 45)
	pid := state.FocusedPane()
	snapshot := blankSnap()
	snapshot.Graphics = []kitty.Placement{{Image: kitty.Image{ID: 8, Width: 100, Height: 40}, Cols: 127, Rows: 40}}
	in := Input{ServerUI: true, State: state, Panes: map[int]*PaneSnap{pid: snapshot}}
	base := Compose(in)
	in.Toast = &Toast{Title: "notice", Body: "context"}
	out := Compose(in)
	if out.ToastRect.W != 0 || out.ToastRect.H != 0 || !reflect.DeepEqual(out.Frame.Graphics, base.Frame.Graphics) {
		t.Fatal("position-less endpoint toast displayed or hid image cells")
	}
	in.Toast.Position = "top-right"
	out = Compose(in)
	if out.ToastRect.W == 0 || reflect.DeepEqual(out.Frame.Graphics, base.Frame.Graphics) {
		t.Fatal("positioned endpoint toast did not display/crop")
	}
}

func TestEndpointModeFooterAndVisibleNoticeCoverOnlyTheirCells(t *testing.T) {
	state := testState(160, 45)
	state.PaneBorders = false
	pid := state.FocusedPane()
	snapshot := blankSnap()
	snapshot.Graphics = []kitty.Placement{{Image: kitty.Image{ID: 8, Width: 100, Height: 40}, Cols: 200, Rows: 200}}
	base := Input{ServerUI: true, State: state, Panes: map[int]*PaneSnap{pid: snapshot}}
	before := graphicCells(Compose(base).Frame.Graphics, pid)
	for _, name := range []string{"resize-footer", "notice"} {
		t.Run(name, func(t *testing.T) {
			in := base
			var covered image.Rectangle
			if name == "resize-footer" {
				in.ModeHint = " RESIZE  h/l width  j/k height  esc exit "
				area := state.PanesArea()
				covered = image.Rect(area.X, area.Y+area.H-1, area.X+area.W, area.Y+area.H)
			} else {
				in.ConfigWarning = "visible notice"
			}
			out := Compose(in)
			if name == "notice" {
				r := out.ConfigWarningRect
				covered = image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)
			}
			want := map[image.Point]bool{}
			removed := 0
			for point := range before {
				if point.In(covered) {
					removed++
				} else {
					want[point] = true
				}
			}
			if removed == 0 {
				t.Fatal("test image does not overlap the visible footer/notice")
			}
			if got := graphicCells(out.Frame.Graphics, pid); !reflect.DeepEqual(got, want) {
				t.Fatal("footer/notice clipped cells outside its visible rectangle")
			}
			if restored := graphicCells(Compose(base).Frame.Graphics, pid); !reflect.DeepEqual(restored, before) {
				t.Fatal("closing footer/notice did not restore source cells")
			}
		})
	}
}

func TestBottomToastAvoidsConfigurationWarningByOneRow(t *testing.T) {
	view := layout.Rect{W: 80, H: 25}
	for _, position := range []string{"bottom-left", "bottom-right"} {
		toast := &Toast{Title: "notice", Position: position}
		plain := ToastRect(toast, view, layout.Rect{})
		warned := ToastRect(toast, view, layout.Rect{W: 80, H: 1})
		if warned.Y != plain.Y-1 || warned.H != plain.H || warned.X != plain.X {
			t.Fatal("bottom toast did not avoid config warning by one row", plain, warned)
		}
	}
}
