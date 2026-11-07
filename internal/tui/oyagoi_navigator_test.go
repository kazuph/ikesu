package tui

import (
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func TestOyagoiNavigatorMovesWorkspaceAndKeepsSelectionTarget(t *testing.T) {
	a, _ := worktreeApp(t)
	a.st.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	want := a.st.Workspaces[1].ID
	a.oyagoiWorkspace = want
	a.navigator = &navigatorView{}
	rows, _ := a.navigatorRows()
	if len(rows) == 0 || rows[0].Workspace != 1 || !strings.Contains(rows[0].Name, "oyagoi") {
		t.Fatalf("HQ not first: %+v", rows)
	}
	a.navigator.selected = 0
	a.navigatorSelect()
	if a.st.ActiveWorkspace().ID != want {
		t.Fatal("navigator selected a different workspace")
	}
}

func TestOyagoiMobileTapSelectsOtherMachine(t *testing.T) {
	a, _ := worktreeApp(t)
	a.st.Cols, a.st.Rows = 40, 35
	a.st.MobileWidthThreshold = 60
	a.mobileSwitcher = true
	a.oyagoiForeignName, a.oyagoiForeignOnline = "remote · mini", true
	var action string
	a.machineRequest = func(s string) error { action = s; return nil }
	sidebarEvent(a, input.KindMouseClick, 4, render.MobileViewport(a.st).Y+1)
	if action != "oyagoi:select" || a.mobileSwitcher {
		t.Fatalf("action=%q open=%v", action, a.mobileSwitcher)
	}
}

func TestOyagoiNavigatorSelectsOtherMachine(t *testing.T) {
	a, _ := worktreeApp(t)
	var action string
	a.machineRequest = func(s string) error { action = s; return nil }
	a.oyagoiForeignName, a.oyagoiForeignOnline = "remote HQ · mini", true
	a.navigator = &navigatorView{}
	rows, _ := a.navigatorRows()
	if len(rows) == 0 || rows[0].Pane != -3 || !strings.Contains(rows[0].Name, "remote HQ") {
		t.Fatalf("foreign HQ absent: %+v", rows)
	}
	a.navigatorSelect()
	if action != "oyagoi:select" {
		t.Fatalf("selection action = %q", action)
	}
}
