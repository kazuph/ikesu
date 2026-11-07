package tui

import (
	"fmt"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
)

func TestWorkspaceMenuRequestsOyagoiSelection(t *testing.T) {
	a, _ := worktreeApp(t)
	var requested string
	a.machineRequest = func(action string) error { requested = action; return nil }
	ws := a.st.Workspaces[0]
	for _, tc := range []struct{ label, action string }{{"🎏 HQ", "set"}} {
		a.openWorkspaceMenu(1, 1, 0)
		activateLabel(t, a, tc.label)
		want := fmt.Sprintf("oyagoi:%s:%d:%s", tc.action, ws.ID, ws.Generation)
		if requested != want {
			t.Fatalf("%s: got %q, want %q", tc.label, requested, want)
		}
	}
}

func TestWorkspaceSectionFailureKeepsHQ(t *testing.T) {
	a, _ := worktreeApp(t)
	ws := a.st.Workspaces[0]
	a.oyagoiWorkspace, a.oyagoiGeneration = ws.ID, ws.Generation
	before := ws.Section
	a.setWorkspaceMenuSection(-1, model.SectionWork)
	if a.dialog == nil || a.oyagoiWorkspace != ws.ID || a.oyagoiGeneration != ws.Generation || ws.Section != before {
		t.Fatal("invalid workspace changed HQ or its section, or did not show an error")
	}
}
