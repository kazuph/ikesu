package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"testing"
)

func TestStage5LastWorkspaceCloseRecreatesHome(t *testing.T) {
	teststate.Lock(t)
	for _, ui := range []bool{false, true} {
		a := specRuntimeApp(t)
		if err := a.StartInitialPane(); err != nil {
			t.Fatal(err)
		}
		a.workspaceNames = map[int]string{0: "stale-cwd"}
		old := a.focusedPane()
		ws := a.st.ActiveWorkspace().ID
		if ui {
			a.openCloseWorkspaceDialog(0)
			a.dialogKey(uv.Key{Code: uv.KeyEnter})
		} else {
			a.ExecCmd(model.CmdCloseWorkspace{Index: 0})
		}
		home, _ := os.UserHomeDir()
		if a.quit || len(a.st.Workspaces) != 1 || a.st.ActiveWorkspace().ID <= ws || a.st.ActiveWorkspace().Cwd != home || a.sup.Get(old) != nil || a.sup.Get(a.focusedPane()) == nil || a.workspaceNames[0] != "" {
			t.Fatalf("ui=%v quit=%v workspaces=%+v", ui, a.quit, a.st.Workspaces)
		}
	}
}

func TestStage5HomeSpawnFailureKeepsLastWorkspace(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	old := a.focusedPane()
	ws := a.st.ActiveWorkspace().ID
	t.Setenv("HOME", t.TempDir()+"/missing-home")
	a.ExecCmd(model.CmdCloseWorkspace{Index: 0})
	if a.lastSpawnError == nil || a.quit || len(a.st.Workspaces) != 1 || a.st.ActiveWorkspace().ID != ws || a.focusedPane() != old || a.sup.Get(old) == nil || a.toast == nil || !a.toast.shown || a.toast.kind != "error" || a.toast.body != a.lastSpawnError.Error() {
		t.Fatal("failed home spawn must keep original workspace and terminal")
	}
}
