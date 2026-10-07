package tui

import (
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
)

// innerRect must never return a non-positive size: a too-narrow terminal
// (sidebar taking the whole width) used to pass negative dimensions into
// vt.NewEmulator and panic.
func TestInnerRectClampsTinyPanes(t *testing.T) {
	for _, cols := range []int{30, 10, 4, 1} {
		st := model.New("", cols, 5)
		a := &App{st: st}
		pid := st.Workspaces[0].Tabs[0].Layout.Focused()
		r := a.innerRect(pid)
		if r.W < 1 || r.H < 1 {
			t.Fatalf("cols=%d: innerRect %+v has non-positive size", cols, r)
		}
	}
}

// G3 fail-closed restart warning: with a missing-session warning the
// dialog switches title and lists the pane; without warnings it keeps the
// normal restart wording.
func TestRestartDialogWarnings(t *testing.T) {
	st := model.New("/tmp", 120, 30)
	a := &App{st: st}

	a.warningsHook = func() []api.AgentSessionWarningInfo {
		return []api.AgentSessionWarningInfo{{
			WorkspaceNumber: 1, WorkspaceLabel: "w1", PaneLabel: "p2",
			Agent: "claude", Cwd: "/tmp/x", Reason: "missing session id",
		}}
	}
	a.openRestartDialog()
	if a.dialog == nil {
		t.Fatal("no dialog")
	}
	if a.dialog.d.Title != "Restart with missing agent sessions?" {
		t.Fatalf("title = %q", a.dialog.d.Title)
	}
	if a.dialog.d.Detail != "These AI panes do not have a recorded session id:" {
		t.Fatalf("detail = %q", a.dialog.d.Detail)
	}
	if len(a.dialog.d.Lines) != 1 || !strings.Contains(a.dialog.d.Lines[0], "missing session id") {
		t.Fatalf("lines = %v", a.dialog.d.Lines)
	}
	if a.dialog.d.Width != 76 {
		t.Fatalf("width = %d, want 76", a.dialog.d.Width)
	}

	a2 := &App{st: st}
	a2.warningsHook = func() []api.AgentSessionWarningInfo { return nil }
	a2.openRestartDialog()
	if a2.dialog.d.Title != "Restart gtx?" || len(a2.dialog.d.Lines) != 0 {
		t.Fatalf("unexpected warning form: %+v", a2.dialog.d)
	}
}
