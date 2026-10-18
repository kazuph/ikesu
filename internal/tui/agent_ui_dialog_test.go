package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestParityNameInputAppendsUnicodeAndClearsWithControlC(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.st.Workspaces[0].Name = "a界b"
	a.st.Workspaces[0].CustomName = true
	a.renameWorkspaceDialog(0)
	if a.dialog.d.InputCursor != 3 {
		t.Fatal("initial cursor")
	}
	a.dialogKey(uv.Key{Code: uv.KeyLeft})
	a.dialogKey(uv.Key{Code: 'x', Text: "x"})
	if a.dialog.d.Input != "a界bx" || a.dialog.d.InputCursor != 4 {
		t.Fatal(a.dialog.d)
	}
	a.dialogKey(uv.Key{Code: uv.KeyBackspace})
	a.dialogKey(uv.Key{Code: uv.KeyDelete})
	if a.dialog.d.Input != "a界b" || a.dialog.d.InputCursor != 3 {
		t.Fatal(a.dialog.d)
	}
	a.dialogKey(uv.Key{Code: uv.KeyHome})
	a.dialogKey(uv.Key{Code: 'y', Text: "y"})
	a.dialogKey(uv.Key{Code: 'c', Mod: uv.ModCtrl})
	if a.dialog.d.Input != "" || a.dialog.d.InputCursor != 0 {
		t.Fatal("Ctrl-C must clear the name without closing the input")
	}
	a.dialogKey(uv.Key{Code: 'y', Text: "a界by"})
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if a.st.Workspaces[0].Name != "a界by" {
		t.Fatal(a.st.Workspaces[0].Name)
	}
}

func TestEndpointStopConfirmationBodyKeepsDialogAndCancelUsesTerminalPosition(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.SetServerUI()
	a.openQuitDialog()
	dialog := a.dialog
	r, cancel, _ := render.DialogRects(&dialog.d, layout.Rect{W: a.st.Cols, H: a.st.Rows})
	for _, kind := range []input.Kind{input.KindMouseClick, input.KindMouseRelease} {
		a.onMouse(input.Event{Kind: kind, Mouse: uv.Mouse{X: r.X + r.W/2, Y: r.Y + 2, Button: uv.MouseLeft}})
		if a.dialog != dialog {
			t.Fatal("endpoint confirmation body must keep the dialog")
		}
	}
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: cancel.X, Y: cancel.Y, Button: uv.MouseLeft}})
	if a.dialog != nil || a.quit {
		t.Fatal("endpoint cancel did not close safely")
	}
}

func TestParityConfirmationRightClickKeepsDialog(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.openQuitDialog()
	dialog := a.dialog
	r, _, _ := render.DialogRects(&dialog.d, a.st.PanesArea())
	m := uv.Mouse{X: r.X + r.W/2, Y: r.Y + 1, Button: uv.MouseRight}
	for _, kind := range []input.Kind{input.KindMouseClick, input.KindMouseRelease} {
		a.onMouse(input.Event{Kind: kind, Mouse: m})
		if a.dialog != dialog {
			t.Fatal("right press/release must keep the confirmation")
		}
	}
}

func TestParityStopConfirmationCancelReturnsToNavigate(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.openQuitDialog()
	a.dialogKey(uv.Key{Code: uv.KeyEscape})
	if a.dialog != nil || !a.workspaceNavigate || a.navigateSelected != a.st.Active {
		t.Fatal("cancel must return to workspace navigation")
	}
	a.openQuitDialog()
	if a.workspaceNavigate {
		t.Fatal("reopened confirmation must hide navigation mode")
	}
	r, cancel, _ := render.DialogRects(&a.dialog.d, a.st.PanesArea())
	for _, point := range []uv.Mouse{{X: cancel.X, Y: cancel.Y, Button: uv.MouseLeft},
		{X: r.X + 1, Y: r.Y + 1, Button: uv.MouseLeft}} {
		a.openQuitDialog()
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: point})
		if a.dialog != nil || !a.workspaceNavigate {
			t.Fatal("cancel button and body press must return to navigation")
		}
	}
}

func TestStage6aNarrowNameInputClickTargets(t *testing.T) {
	teststate.Lock(t)
	for _, width := range []int{20, 30, 40, 56} {
		a := specRuntimeApp(t)
		a.st.Cols, a.st.Rows = width, 7
		for _, point := range []uv.Mouse{{X: width/2 - 1, Y: 3, Button: uv.MouseLeft}, {X: 4, Y: 2, Button: uv.MouseLeft}, {X: 0, Y: 0, Button: uv.MouseLeft}} {
			a.renameWorkspaceDialog(0)
			d := a.dialog
			a.dialogMouse(input.Event{Kind: input.KindMouseClick, Mouse: point})
			if a.dialog != d {
				t.Fatalf("width %d non-button click dismissed input at %+v", width, point)
			}
		}
		a.renameWorkspaceDialog(0)
		d := a.dialog
		a.dialogMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: 4, Y: 4, Button: uv.MouseLeft}})
		if width == 56 && a.dialog != d {
			t.Fatal("left padding dismissed input")
		}
		if width < 56 && a.dialog != nil {
			t.Fatalf("width %d observed save hit did not commit", width)
		}
	}
}
