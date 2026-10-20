package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
)

func TestParityPopupOutsideLeftClosesAndConsumes(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	owner := a.st.ActiveWorkspace().ID
	root := a.focusedPane()
	width, height := api.PopupSize{Cells: 40}, api.PopupSize{Cells: 12}
	if _, err := a.OpenPopup(a.cwd, "exec sleep 60", &width, &height); err != nil {
		t.Fatal(err)
	}
	popup := a.st.Popup.PaneID
	r := a.st.PopupRect()
	m := uv.Mouse{X: a.st.Cols - 2, Y: a.st.Rows - 3, Button: uv.MouseLeft}
	if pointIn(m.X, m.Y, r) {
		t.Fatal("test point must be outside")
	}
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
	a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
	if a.st.Popup != nil || a.sup.Get(popup) != nil || a.focusedPane() != root || a.st.ActiveWorkspace().ID != owner || a.sel != nil {
		t.Fatal("outside click must close only the popup and consume the click")
	}
}
