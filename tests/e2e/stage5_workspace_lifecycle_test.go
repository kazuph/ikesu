package e2e

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	saved "github.com/kazuph/goterrace/internal/session"
)

func TestStage5LastWorkspaceCloseSaveRestore(t *testing.T) { stage5CloseLifecycle(t, false) }
func TestStage5LastWorkspaceCloseLiveHandoff(t *testing.T) { stage5CloseLifecycle(t, true) }

func stage5CloseLifecycle(t *testing.T, handoff bool) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := startEnv(t, map[string]string{"HOME": home})
	s.waitFor("initial shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	var initial api.WorkspaceListResult
	s.rpc(api.MethodWorkspaceList, api.EmptyParams{}, &initial)
	var old api.PaneListResult
	s.rpc(api.MethodPaneList, api.EmptyParams{}, &old)
	s.rpc(api.MethodWorkspaceClose, api.WorkspaceTarget{WorkspaceID: initial.Workspaces[0].WorkspaceID}, nil)
	var before api.PaneListResult
	s.rpc(api.MethodPaneList, api.EmptyParams{}, &before)
	if len(before.Panes) != 1 || before.Panes[0].PaneID == old.Panes[0].PaneID || before.Panes[0].WorkspaceID == old.Panes[0].WorkspaceID {
		t.Fatalf("old identity retained: %+v -> %+v", old, before)
	}
	p := before.Panes[0]
	if p.Cwd == nil || *p.Cwd != home {
		t.Fatalf("new pane cwd: %+v expected %s", p, home)
	}
	s.rpc(api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: p.PaneID, Text: "STAGE5_TOKEN=retained; printf 'READY_%s\\n' NEW_HOME", Keys: []string{"enter"}}, nil)
	s.waitFor("new home shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "READY_NEW_HOME") })
	operation := "restart"
	if handoff {
		operation = "live-handoff"
	}
	if out, err := s.cli("server", operation); err != nil {
		t.Fatalf("%s: %v %s", operation, err, out)
	}
	s.waitFor("reconnected home shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	var after api.PaneListResult
	s.rpc(api.MethodPaneList, api.EmptyParams{}, &after)
	if len(after.Panes) != 1 || after.Panes[0].PaneID != p.PaneID || after.Panes[0].TerminalID != p.TerminalID || after.Panes[0].WorkspaceID != p.WorkspaceID || after.Panes[0].Cwd == nil || *after.Panes[0].Cwd != home {
		t.Fatalf("identity/home lost: %+v -> %+v", before, after)
	}
	var workspaces api.WorkspaceListResult
	s.rpc(api.MethodWorkspaceList, api.EmptyParams{}, &workspaces)
	if len(workspaces.Workspaces) != 1 || workspaces.Workspaces[0].WorkspaceID != p.WorkspaceID {
		t.Fatal(workspaces)
	}
	if !handoff {
		f, err := saved.Load(filepath.Join(s.stateDir, s.name, saved.SessionFileName))
		if err != nil || f == nil {
			t.Fatalf("saved session: %+v %v", f, err)
		}
		if len(f.Workspaces) != 1 || f.Workspaces[0].Cwd != home || len(f.Workspaces[0].Tabs) != 1 || len(f.Workspaces[0].Tabs[0].Panes) != 1 || fmt.Sprintf("p%d", f.Workspaces[0].Tabs[0].Panes[0].ID) != p.PaneID {
			t.Fatalf("saved intermediate/old workspace: %+v", f)
		}
	}
	command := "printf 'RESTORED_%s\\n' INPUT_OK"
	marker := "RESTORED_INPUT_OK"
	if handoff {
		command = "printf 'HANDOFF_%s_%s\\n' INPUT_OK \"$STAGE5_TOKEN\""
		marker = "HANDOFF_INPUT_OK_retained"
	}
	s.rpc(api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: p.PaneID, Text: command, Keys: []string{"enter"}}, nil)
	s.waitFor("successor executes input", 15*time.Second, func(tx string) bool { return strings.Contains(tx, marker) })
	t.Logf("%s: old=%s/%s absent; new=%s/%s terminal=%s HOME=%s; one workspace/pane; actual shell output=%s", operation, old.Panes[0].WorkspaceID, old.Panes[0].PaneID, p.WorkspaceID, p.PaneID, p.TerminalID, home, marker)
}
