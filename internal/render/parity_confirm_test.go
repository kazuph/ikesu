package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/layout"
	"testing"
)

func TestParityConfirmationPaneAreaAndButtons(t *testing.T) {
	d := &Dialog{Title: "Stop server?", Detail: "Stops the ikesu server and all running panes.", Confirm: "stop", Cancel: "cancel", Width: 76}
	for _, tc := range []struct{ area, outer, confirm, cancel layout.Rect }{
		{layout.Rect{X: 26, Y: 1, W: 134, H: 43}, layout.Rect{X: 55, Y: 20, W: 76, H: 5}, layout.Rect{X: 80, Y: 23, W: 11, H: 1}, layout.Rect{X: 93, Y: 23, W: 12, H: 1}},
		{layout.Rect{X: 26, Y: 1, W: 74, H: 23}, layout.Rect{X: 28, Y: 10, W: 70, H: 5}, layout.Rect{X: 50, Y: 13, W: 11, H: 1}, layout.Rect{X: 63, Y: 13, W: 12, H: 1}},
	} {
		outer, cancel, confirm := DialogRects(d, tc.area)
		if outer != tc.outer || confirm != tc.confirm || cancel != tc.cancel {
			t.Fatal(outer, confirm, cancel)
		}
		f := NewFrame(tc.area.X+tc.area.W, tc.area.Y+tc.area.H+1)
		out := &Output{}
		drawDialog(f, d, out, tc.area)
		if out.DialogRect != outer || out.DialogConfirm != confirm || out.DialogCancel != cancel {
			t.Fatal("draw and click geometry differ")
		}
		if got := f.Cell(confirm.X, confirm.Y).Style; got.Fg != f.Palette.PopupBG || got.Bg != f.Palette.Red || got.Attrs != uv.AttrBold {
			t.Fatal(got)
		}
		if got := f.Cell(cancel.X, cancel.Y).Style; got.Fg != f.Palette.Text || got.Bg != f.Palette.Surface || got.Attrs != uv.AttrBold {
			t.Fatal(got)
		}
	}
}

func TestEndpointRestoreCompactButtonsKeepWholeTerminalCenter(t *testing.T) {
	d := &Dialog{Title: "Restore agents?", Detail: "Types resume commands into panes with recorded agent sessions.", Confirm: "restore", Cancel: "cancel", Width: 76, CenterTerminal: true, CompactButtons: true}
	outer, cancel, confirm := DialogRects(d, layout.Rect{W: 160, H: 45})
	if outer != (layout.Rect{X: 42, Y: 20, W: 76, H: 5}) || confirm != (layout.Rect{X: 67, Y: 23, W: 11, H: 1}) || cancel != (layout.Rect{X: 80, Y: 23, W: 12, H: 1}) {
		t.Fatal("restore controls differ from recorded endpoint coordinates", outer, confirm, cancel)
	}
}
