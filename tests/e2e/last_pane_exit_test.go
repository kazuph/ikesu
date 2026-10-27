package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestLastShellExitClosesWorkspaceWithViewer(t *testing.T) {
	s := start(t)
	s.waitFor("initial shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	var survivor api.WorkspaceCreatedResult
	s.rpc(api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Focus: false}, &survivor)
	var before api.WorkspaceListResult
	s.rpc(api.MethodWorkspaceList, api.EmptyParams{}, &before)
	t.Logf("before exit: %+v", before)
	s.send("exit\r")
	s.waitFor("last shell exit closes its workspace", 15*time.Second, func(text string) bool {
		var spaces api.WorkspaceListResult
		s.rpc(api.MethodWorkspaceList, api.EmptyParams{}, &spaces)
		return len(spaces.Workspaces) == 1 && spaces.Workspaces[0].WorkspaceID == survivor.Workspace.WorkspaceID
	})
	var panes api.PaneListResult
	s.rpc(api.MethodPaneList, api.PaneListParams{}, &panes)
	if len(panes.Panes) != 1 || panes.Panes[0].PaneID != survivor.RootPane.PaneID {
		t.Fatal("surviving pane changed", panes)
	}
	t.Logf("after exit panes: %+v; screen:\n%s", panes, s.text())
}

func TestFallbackShellExitClosesWorkspaceWithViewer(t *testing.T) {
	s := start(t)
	s.waitFor("initial shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	var survivor api.WorkspaceCreatedResult
	s.rpc(api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Focus: false}, &survivor)
	sessionID := "last-pane-retained-session"
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "test", Agent: "codex", State: "working", SessionID: &sessionID}, nil)
	s.send("exit\r")
	s.waitFor("agent exit retains a shell", 15*time.Second, func(text string) bool {
		var panes api.PaneListResult
		s.rpc(api.MethodPaneList, api.PaneListParams{}, &panes)
		for _, p := range panes.Panes {
			if p.PaneID == "p1" {
				return p.AgentStatus == "unknown" && p.AgentSession != nil
			}
		}
		return false
	})
	// A fresh shell must accept a command before the user's second exit.
	s.send("printf 'FALLBACK_%s\\n' READY\r")
	s.waitFor("retained pane's shell ready", 15*time.Second, func(text string) bool { return strings.Contains(text, "FALLBACK_READY") })
	var retained api.PaneListResult
	s.rpc(api.MethodPaneList, api.PaneListParams{}, &retained)
	t.Logf("retained after agent exit: %+v", retained)
	s.send("exit\r")
	s.waitFor("explicit exit from fallback shell closes workspace", 15*time.Second, func(text string) bool {
		var spaces api.WorkspaceListResult
		s.rpc(api.MethodWorkspaceList, api.EmptyParams{}, &spaces)
		return len(spaces.Workspaces) == 1 && spaces.Workspaces[0].WorkspaceID == survivor.Workspace.WorkspaceID
	})
}
