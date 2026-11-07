package tui

import (
	"fmt"
	"testing"
)

func TestWorkspaceMenuRequestsOyagoiSelection(t *testing.T) {
	a, _ := worktreeApp(t)
	var requested string
	a.machineRequest = func(action string) error { requested = action; return nil }
	ws := a.st.Workspaces[0]
	for _, tc := range []struct{ label, action string }{{"set as HQ", "set"}, {"unset HQ", "unset"}} {
		a.openWorkspaceMenu(1, 1, 0)
		activateLabel(t, a, tc.label)
		want := fmt.Sprintf("oyagoi:%s:%d:%s", tc.action, ws.ID, ws.Generation)
		if requested != want {
			t.Fatalf("%s: got %q, want %q", tc.label, requested, want)
		}
	}
}
