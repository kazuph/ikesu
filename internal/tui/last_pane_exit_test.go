package tui

import (
	"os"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestLastShellExitReplacesFinalWorkspace(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	old, space := a.focusedPane(), a.st.ActiveWorkspace().ID
	p := a.sup.Get(old)
	a.sup.Input(old, []byte("exit\r"))
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-a.sup.Events():
			a.onRuntimeEvent(ev)
			if ev.Kind == runtime.EventExited && ev.Pane == old {
				home, _ := os.UserHomeDir()
				if a.quit || len(a.st.Workspaces) != 1 || a.st.ActiveWorkspace().ID == space || a.focusedPane() == old || a.st.ActiveWorkspace().Cwd != home || a.sup.Get(old) == p {
					t.Fatal("final shell exit must replace its workspace with a fresh home shell", a.quit, a.st.Workspaces)
				}
				return
			}
		case <-deadline:
			t.Fatal("real shell did not exit")
		}
	}
}
