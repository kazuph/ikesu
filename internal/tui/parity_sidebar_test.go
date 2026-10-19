package tui

import (
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestParityCollapsedSidebarUsesFirstTerminalRowActualPTY(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.effect(model.EffSpawnPane{Pane: a.focusedPane(), Cwd: a.cwd})
	a.exec(model.CmdNewWorkspace{Cwd: a.cwd})
	a.exec(model.CmdNewWorkspace{Cwd: a.cwd})
	a.exec(model.CmdToggleSidebar{})
	if a.st.SidebarArea().W != 2 {
		t.Fatal(a.st.SidebarArea())
	}
	sidebarEvent(a, input.KindMouseClick, 0, 0)
	if a.st.Active != 0 {
		t.Fatal("first collapsed row did not select first workspace", a.st.Active)
	}
	sidebarEvent(a, input.KindMouseClick, 0, 1)
	if a.st.Active != 1 {
		t.Fatal("second collapsed row did not select second workspace", a.st.Active)
	}
	if a.sup.Get(a.focusedPane()) == nil {
		t.Fatal("selected workspace has no real PTY")
	}
}
