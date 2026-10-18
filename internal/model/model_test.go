package model

import (
	"testing"

	"github.com/kazuph/goterrace/internal/layout"
)

func newState() *AppState { return New("/tmp", 120, 40) }

func findEffect[T Effect](effs []Effect) (T, bool) {
	var zero T
	for _, e := range effs {
		if v, ok := e.(T); ok {
			return v, true
		}
	}
	return zero, false
}

// A new workspace gains one pane and emits a spawn effect for it.
func TestNewWorkspace(t *testing.T) {
	s := newState()
	effs := s.Apply(CmdNewWorkspace{Cwd: "/work"})
	if len(s.Workspaces) != 2 {
		t.Fatalf("workspaces = %d, want 2", len(s.Workspaces))
	}
	if s.Active != 1 {
		t.Fatalf("active = %d, want 1", s.Active)
	}
	ws := s.Workspaces[1]
	if ws.Tabs[0].Layout.PaneCount() != 1 {
		t.Fatalf("pane count = %d", ws.Tabs[0].Layout.PaneCount())
	}
	spawn, ok := findEffect[EffSpawnPane](effs)
	if !ok || spawn.Cwd != "/work" {
		t.Fatalf("effects = %v, want EffSpawnPane{cwd:/work}", effs)
	}
}

// Closing a pane emits a kill effect; closing the last pane of a workspace
// removes the workspace.
func TestClosePaneRemovesEmptyWorkspace(t *testing.T) {
	s := newState()
	s.Apply(CmdNewWorkspace{Cwd: "/w2"})
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})

	ws := s.Workspaces[1]
	panes := ws.Tabs[0].Layout.PaneIDs()
	effs := s.Apply(CmdClosePane{Pane: panes[1]})
	if _, ok := findEffect[EffKillPane](effs); !ok {
		t.Fatalf("effects = %v, want EffKillPane", effs)
	}
	if ws.Tabs[0].Layout.PaneCount() != 1 {
		t.Fatalf("pane count = %d", ws.Tabs[0].Layout.PaneCount())
	}

	effs = s.Apply(CmdClosePane{Pane: panes[0]})
	if len(s.Workspaces) != 1 {
		t.Fatalf("workspaces = %d, want 1 after last pane closed", len(s.Workspaces))
	}
	if s.Active != 0 {
		t.Fatalf("active = %d, want 0", s.Active)
	}
	if _, ok := findEffect[EffQuit](effs); ok {
		t.Fatal("closing a non-last workspace must not quit")
	}
}

// Closing the last pane of the last workspace quits the app.
func TestLastPaneQuits(t *testing.T) {
	s := newState()
	pane := s.Workspaces[0].Tabs[0].Layout.PaneIDs()[0]
	effs := s.Apply(CmdClosePane{Pane: pane})
	if _, ok := findEffect[EffQuit](effs); !ok {
		t.Fatalf("effects = %v, want EffQuit", effs)
	}
	if len(s.Workspaces) != 0 {
		t.Fatalf("workspaces = %d, want 0", len(s.Workspaces))
	}
}

// Closing every workspace quits the app.
func TestCloseAllWorkspacesQuits(t *testing.T) {
	s := newState()
	s.Apply(CmdNewWorkspace{Cwd: "/w2"})
	s.Apply(CmdCloseWorkspace{Index: 0})
	if len(s.Workspaces) != 1 {
		t.Fatalf("workspaces = %d", len(s.Workspaces))
	}
	effs := s.Apply(CmdCloseWorkspace{Index: 0})
	if _, ok := findEffect[EffQuit](effs); !ok {
		t.Fatalf("effects = %v, want EffQuit", effs)
	}
}

// Workspace selection changes which layout is active.
func TestSelectWorkspace(t *testing.T) {
	s := newState()
	s.Apply(CmdNewWorkspace{Cwd: "/w2"})
	s.Apply(CmdSelectWorkspace{Index: 0})
	if s.Active != 0 {
		t.Fatalf("active = %d", s.Active)
	}
	// Out of range is ignored.
	s.Apply(CmdSelectWorkspace{Index: 9})
	if s.Active != 0 {
		t.Fatalf("active = %d after invalid select", s.Active)
	}
}

// Splitting emits spawn for the new pane, focused on it.
func TestSplitFocused(t *testing.T) {
	s := newState()
	effs := s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	spawn, ok := findEffect[EffSpawnPane](effs)
	if !ok {
		t.Fatalf("effects = %v", effs)
	}
	if s.FocusedPane() != spawn.Pane {
		t.Fatalf("focus = %d, want new pane %d", s.FocusedPane(), spawn.Pane)
	}
}

// Input produces a send effect; focus commands move layout focus.
func TestInputAndFocus(t *testing.T) {
	s := newState()
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	effs := s.Apply(CmdInput{Pane: 1, Bytes: []byte("ls\n")})
	in, ok := findEffect[EffInput](effs)
	if !ok || in.Pane != 1 || string(in.Bytes) != "ls\n" {
		t.Fatalf("effects = %v", effs)
	}
	s.Apply(CmdFocusDir{Dir: layout.NavLeft})
	if s.FocusedPane() != 1 {
		t.Fatalf("focus = %d, want 1", s.FocusedPane())
	}
	s.Apply(CmdCyclePane{Next: true})
	if s.FocusedPane() != 2 {
		t.Fatalf("focus = %d, want 2", s.FocusedPane())
	}
}

// Sidebar toggle and resize produce layout syncs and shrink the pane area.
func TestSidebarAndResize(t *testing.T) {
	s := newState()
	before := s.PanesArea()
	s.Apply(CmdToggleSidebar{})
	after := s.PanesArea()
	if after.W <= before.W {
		t.Fatalf("collapsed sidebar (2 cells) should widen panes area: %d -> %d", before.W, after.W)
	}
	s.Apply(CmdResize{Cols: 100, Rows: 30})
	// Distribution capture: tab and action bar leave Rows-2 to desktop panes.
	if s.PanesArea().W != 100-SidebarCollapsedWidth-s.RightSidebarArea().W || s.PanesArea().H != 28 {
		t.Fatalf("panes area after resize = %+v", s.PanesArea())
	}
}
