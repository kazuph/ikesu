package e2e

import (
	"github.com/kazuph/ikesu/internal/dispatch"
	persisted "github.com/kazuph/ikesu/internal/session"
	"os"
	"path/filepath"
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
	if out, err := s.cli("server", "live-handoff"); err != nil {
		t.Fatal(out, err)
	}
	s.send("exit\r")
	s.waitFor("explicit exit from fallback shell closes workspace", 15*time.Second, func(text string) bool {
		var spaces api.WorkspaceListResult
		s.rpc(api.MethodWorkspaceList, api.EmptyParams{}, &spaces)
		return len(spaces.Workspaces) == 1 && spaces.Workspaces[0].WorkspaceID == survivor.Workspace.WorkspaceID
	})
	s.waitFor("deleted workspace absent from saved session", 15*time.Second, func(text string) bool {
		saved, err := persisted.Load(filepath.Join(s.stateDir, s.name, "session.json"))
		return err == nil && saved != nil && len(saved.Workspaces) == 1 && saved.Workspaces[0].ID == 2
	})
	store, err := dispatch.OpenReadOnly(filepath.Join(s.stateDir, "sessions", s.name, "ikesu.db"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.PaneSessions(s.name)
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.PaneID == 1 {
			t.Fatal("deleted pane ledger remains", row)
		}
	}
	for _, operation := range []string{"live-handoff", "restart"} {
		if out, err := s.cli("server", operation); err != nil {
			t.Fatal(out, err)
		}
		var spaces api.WorkspaceListResult
		s.rpc(api.MethodWorkspaceList, api.EmptyParams{}, &spaces)
		var panes api.PaneListResult
		s.rpc(api.MethodPaneList, api.PaneListParams{}, &panes)
		if len(spaces.Workspaces) != 1 || spaces.Workspaces[0].WorkspaceID != survivor.Workspace.WorkspaceID || len(panes.Panes) != 1 || panes.Panes[0].PaneID != survivor.RootPane.PaneID {
			t.Fatal("deleted owner resurrected", operation, spaces, panes)
		}
	}
}

func TestLastPaneExitOtherTabAndViewer(t *testing.T) {
	s := start(t)
	s.waitFor("initial shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	var tab api.TabCreatedResult
	s.rpc(api.MethodTabCreate, api.TabCreateParams{Focus: false}, &tab)
	client, _ := sizeClient(t, s)
	s.send("exit\r")
	s.waitFor("only the exited tab removed", 15*time.Second, func(text string) bool {
		var panes api.PaneListResult
		s.rpc(api.MethodPaneList, api.PaneListParams{}, &panes)
		return len(panes.Panes) == 1 && panes.Panes[0].PaneID == tab.RootPane.PaneID
	})
	marker := filepath.Join(t.TempDir(), "other-viewer-input")
	if _, err := client.WriteString("printf 'OTHER_VIEWER_ALIVE' > '" + marker + "'\r"); err != nil {
		t.Fatal(err)
	}
	s.waitFor("other viewer selects surviving tab", 15*time.Second, func(text string) bool {
		data, _ := os.ReadFile(marker)
		return string(data) == "OTHER_VIEWER_ALIVE"
	})
}

func TestLastPaneExitFinalWorkspaceWithViewer(t *testing.T) {
	s := start(t)
	s.waitFor("initial shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.send("exit\r")
	s.waitFor("new workspace after final shell exit", 15*time.Second, func(text string) bool {
		var spaces api.WorkspaceListResult
		s.rpc(api.MethodWorkspaceList, api.EmptyParams{}, &spaces)
		return len(spaces.Workspaces) == 1 && spaces.Workspaces[0].WorkspaceID != "s1"
	})
}
