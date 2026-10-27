package e2e

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/childenv"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLiveHandoffStructureCountersAndSiblingExit(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "handoff-checkout")
	child := filepath.Join(repo, "shell-directory")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := childenv.Command("git", "-C", repo, "init", "-qb", "main").CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	s := start(t)
	s.waitFor("shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.send(fmt.Sprintf("cd %q; printf 'DIRECTORY_%%s\\n' READY\r", child))
	s.waitFor("shell entered real Git checkout", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "DIRECTORY_READY") })
	var workspace api.WorkspaceCreatedResult
	s.rpc(api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Cwd: &child, Focus: false}, &workspace)
	first := "s1"
	var tab api.TabCreatedResult
	s.rpc(api.MethodTabCreate, api.TabCreateParams{WorkspaceID: &first, Focus: false}, &tab)
	paneID := "p1"
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &paneID, Direction: "right", Focus: false}, nil)
	var before api.WorkspaceListResult
	// Default names resolve asynchronously from basename(cwd) to the Git
	// checkout name. Compare handoff after that legitimate update, and keep
	// exercising automatic names even when the test source is a tar archive.
	s.waitFor("automatic checkout names resolved before handoff", 15*time.Second, func(_ string) bool {
		s.rpc(api.MethodWorkspaceList, api.EmptyParams{}, &before)
		if len(before.Workspaces) != 2 {
			return false
		}
		for _, w := range before.Workspaces {
			if w.Label != filepath.Base(repo) {
				return false
			}
		}
		return true
	})
	var tabs api.TabListResult
	s.rpc(api.MethodTabList, api.TabListParams{WorkspaceID: &first}, &tabs)
	var layout api.LayoutExportResult
	s.rpc(api.MethodLayoutExport, api.LayoutExportParams{PaneID: &paneID}, &layout)
	var panes api.PaneListResult
	s.rpc(api.MethodPaneList, api.PaneListParams{}, &panes)
	if len(panes.Panes) != 4 {
		t.Fatalf("prepared panes: %+v", panes)
	}
	if out, err := s.cli("server", "live-handoff"); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	var after api.WorkspaceListResult
	s.rpc(api.MethodWorkspaceList, api.EmptyParams{}, &after)
	var afterTabs api.TabListResult
	s.rpc(api.MethodTabList, api.TabListParams{WorkspaceID: &first}, &afterTabs)
	var afterLayout api.LayoutExportResult
	s.rpc(api.MethodLayoutExport, api.LayoutExportParams{PaneID: &paneID}, &afterLayout)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(tabs, afterTabs) || !reflect.DeepEqual(layout, afterLayout) {
		t.Fatalf("structure changed: workspaces=%+v/%+v tabs=%+v/%+v layout=%+v/%+v", before, after, tabs, afterTabs, layout, afterLayout)
	}
	var afterPanes api.PaneListResult
	s.rpc(api.MethodPaneList, api.PaneListParams{}, &afterPanes)
	if len(afterPanes.Panes) != len(panes.Panes) {
		t.Fatalf("lost pane: %+v", afterPanes)
	}
	for i, p := range panes.Panes {
		q := afterPanes.Panes[i]
		if p.PaneID != q.PaneID || p.TerminalID != q.TerminalID || p.WorkspaceID != q.WorkspaceID || p.TabID != q.TabID {
			t.Fatalf("pane identity changed: %+v / %+v", p, q)
		}
	}
	var newTab api.TabCreatedResult
	s.rpc(api.MethodTabCreate, api.TabCreateParams{WorkspaceID: &first, Focus: false}, &newTab)
	var newWorkspace api.WorkspaceCreatedResult
	s.rpc(api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Focus: false}, &newWorkspace)
	if newTab.Tab.Number != 3 || newTab.Tab.TabID != "s1:t4" || newTab.RootPane.PaneID != "p5" || newWorkspace.Workspace.WorkspaceID != "s3" || newWorkspace.RootPane.PaneID != "p6" {
		t.Fatalf("counter reset: %+v %+v", newTab, newWorkspace)
	}
	s.rpc(api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: tab.RootPane.PaneID, Text: "exit", Keys: []string{"enter"}}, nil)
	s.waitFor("imported sibling shell exits", 15*time.Second, func(_ string) bool {
		var current api.PaneListResult
		s.rpc(api.MethodPaneList, api.PaneListParams{}, &current)
		for _, p := range current.Panes {
			if p.PaneID == tab.RootPane.PaneID {
				return false
			}
		}
		return len(current.Panes) == 5
	})
	t.Logf("two workspaces, two tabs and split layout retained; next tab=%s pane=%s workspace=%s pane=%s; imported sibling %s exited without closing survivors", newTab.Tab.TabID, newTab.RootPane.PaneID, newWorkspace.Workspace.WorkspaceID, newWorkspace.RootPane.PaneID, tab.RootPane.PaneID)
}
