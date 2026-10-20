package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestStage4RightCollapsedAgentClickSelectsItsPane(t *testing.T) {
	a := specRuntimeApp(t)
	s := a.st
	s.Cols, s.Rows = 160, 45
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	s.RightSidebarMode = "compact"
	s.AgentScope = "sort"
	a.SetServerUI()
	a.SetReported(1, "codex", "idle")
	a.SetReported(2, "codex", "blocked")
	x := s.RightSidebarArea().X + 1
	a.rightSidebarClick(uv.Mouse{X: x, Y: 0, Button: uv.MouseLeft})
	if s.Active != 1 || a.focusedPane() != 2 {
		t.Fatal("first priority status did not select its pane")
	}
	a.rightSidebarClick(uv.Mouse{X: x, Y: 1, Button: uv.MouseLeft})
	if s.Active != 0 || a.focusedPane() != 1 {
		t.Fatal("second status did not select its pane")
	}
	a.rightSidebarClick(uv.Mouse{X: x, Y: 3, Button: uv.MouseLeft})
	if s.Active != 0 {
		t.Fatal("blank right row changed workspace")
	}
}

func TestStage4SelectedWorktreeRowRemainsClickable(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	s.Workspaces[0].Worktree = &api.WorkspaceWorktreeInfo{RepoKey: "repo"}
	s.Workspaces[0].GroupCollapsed = true
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	s.Workspaces[1].Worktree = &api.WorkspaceWorktreeInfo{RepoKey: "repo", IsLinkedWorktree: true}
	a := NewApp(s, runtime.NewSupervisor(), nil, t.TempDir())
	a.SetServerUI()
	a.workspaceNavigate, a.navigateSelected = true, 1
	s.WorkspaceDensity = "slim"
	row, found := a.sidebarRowAt(2, 6)
	if !found || row.Kind != render.RowWorkspace || row.Ws != 1 {
		t.Fatalf("selected child hit target: %+v found=%t", row, found)
	}
}

func TestStage4PanelSwitchResetsAgentButRetainsJobScroll(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	a := NewApp(s, runtime.NewSupervisor(), nil, t.TempDir())
	a.SetServerUI()
	a.agentScroll, a.jobScroll = 1, 2
	y := 0
	x := s.RightSidebarArea().X + 1
	a.rightSidebarClick(uv.Mouse{X: x + 11, Y: y, Button: uv.MouseLeft})
	if !a.jobsPanel || a.agentScroll != 0 || a.jobScroll != 2 {
		t.Fatalf("switch to jobs: agents=%d jobs=%d", a.agentScroll, a.jobScroll)
	}
	a.agentScroll = 1
	a.rightSidebarClick(uv.Mouse{X: x + 2, Y: y, Button: uv.MouseLeft})
	if a.jobsPanel || a.agentScroll != 0 || a.jobScroll != 2 {
		t.Fatalf("switch to agents: agents=%d jobs=%d", a.agentScroll, a.jobScroll)
	}
}
