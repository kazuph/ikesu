package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestParityNameButtonsSaveClearAndCancelWithRealPTY(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	old := a.focusedPane()
	ws := a.st.ActiveWorkspace()
	click := func(button int) {
		r, _, _ := render.DialogRects(&a.dialog.d, layout.Rect{W: a.st.Cols, H: a.st.Rows})
		hit := render.NameInputButtons(r)[button]
		m := uv.Mouse{X: hit.X + 1, Y: hit.Y, Button: uv.MouseLeft}
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
		a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
	}
	specFeed(a, "\x02cNAME_TO_CLEAR")
	click(1)
	if a.dialog == nil || *a.dialog.input != "" || len(ws.Tabs) != 1 || a.focusedPane() != old {
		t.Fatal("clear must keep the input and original shell")
	}
	click(2)
	if a.dialog != nil || len(ws.Tabs) != 1 || a.sup.Get(old) == nil {
		t.Fatal("cancel must retain the original shell")
	}
	specFeed(a, "\x02c\x03TAB_MOUSE_NAME")
	click(0)
	if a.dialog != nil || len(ws.Tabs) != 2 || ws.TabRef().Name != "TAB_MOUSE_NAME" || a.sup.Get(a.focusedPane()) == nil {
		t.Fatal("save must name and select a new real shell")
	}
	a.exec(model.CmdSelectTab{Workspace: 0, Tab: 0})
	if a.focusedPane() != old || a.sup.Get(old) == nil {
		t.Fatal("original shell was lost")
	}
}
