package tui

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestPluginPopupHasNoPaneIdentityActualPTY(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	defer a.sup.KillAll()
	a.sup.Env = func(id int) []string { return []string{"GTX_PANE_ID=p999", "GTX_SOCKET_PATH=/test"} }
	id, err := a.OpenPluginPopup(a.cwd, []string{"/bin/sh", "-c", "if [ -n \"${GTX_PANE_ID+x}\" ]; then printf BAD_ID; else printf NO_PANE_ID; fi; exec /bin/cat"}, map[string]string{"GTX_PANE_ID": "caller-forged"}, nil, nil)
	if err != nil || id == "" {
		t.Fatal(id, err)
	}
	specWaitText(t, a.sup.Get(a.st.Popup.PaneID), "NO_PANE_ID")
}
