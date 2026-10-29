package tui

import (
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
	"io"
	"testing"
)

func TestPluginZoomedNoFocusKeepsWorkspaceAndTab(t *testing.T) {
	st := model.New(t.TempDir(), 120, 40)
	st.Apply(model.CmdNewWorkspace{NoFocus: true})
	sup := runtime.NewSupervisor()
	defer sup.KillAll()
	app := NewApp(st, sup, io.Discard, "")
	beforeWs, beforeTab := st.ActiveWorkspace().ID, st.ActiveWorkspace().TabRef().ID
	target := st.Workspaces[1].Tabs[0].Layout.Focused()
	pid, err := app.OpenPluginPane(1, target, "zoomed", "right", t.TempDir(), []string{"/bin/cat"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	tab := st.Workspaces[1].Tabs[0]
	if tab.Layout.Focused() != pid || tab.Zoomed != pid {
		t.Fatal("zoomed placement lost zoom or tab-local focus")
	}
	if st.ActiveWorkspace().ID != beforeWs || st.ActiveWorkspace().TabRef().ID != beforeTab {
		t.Fatal("no-focus moved workspace/tab")
	}
	focused, err := app.OpenPluginPane(0, st.FocusedPane(), "zoomed", "right", t.TempDir(), []string{"/bin/cat"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if st.FocusedPane() != focused || st.ActiveWorkspace().TabRef().Zoomed != focused {
		t.Fatal("explicit plugin focus lost zoom")
	}
}
