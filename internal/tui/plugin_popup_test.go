package tui

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
)

func TestPluginPopupHasNoPaneIdentityActualPTY(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	defer a.sup.KillAll()
	a.sup.Env = func(id int) []string { return []string{"IKESU_PANE_ID=p999", "IKESU_SOCKET_PATH=/test"} }
	id, err := a.OpenPluginPopup(a.cwd, []string{"/bin/sh", "-c", "if [ -n \"${IKESU_PANE_ID+x}\" ]; then printf BAD_ID; else printf NO_PANE_ID; fi; exec /bin/cat"}, map[string]string{"IKESU_PANE_ID": "caller-forged"}, nil, nil)
	if err != nil || id == "" {
		t.Fatal(id, err)
	}
	specWaitText(t, a.sup.Get(a.st.Popup.PaneID), "NO_PANE_ID")
	a.ExecCmd(model.CmdNewWorkspace{Cwd: a.cwd})
	second, err := a.OpenPluginPopup(a.cwd, []string{"/bin/sh", "-c", "printf SECOND_POPUP; exec /bin/cat"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, a.sup.Get(a.st.Popup.PaneID), "SECOND_POPUP")
	if second == id || a.PopupTerminal(id) == nil {
		t.Fatal("hidden popup terminal unavailable")
	}
	if text := a.PopupSelectionText(id, api.PaneTextPoint{Row: 0, Col: 0}, api.PaneTextPoint{Row: 0, Col: 9}); text != "NO_PANE_ID" {
		t.Fatal("selection read wrong popup", text)
	}

}
