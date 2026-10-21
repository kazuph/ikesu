package model

import (
	"reflect"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
)

func TestStage4CollapsedWorktreeKeepsActiveChild(t *testing.T) {
	s := New(t.TempDir(), 160, 45)
	s.Apply(CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	s.Apply(CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	s.Workspaces[0].Worktree = &api.WorkspaceWorktreeInfo{RepoKey: "repo"}
	for _, w := range s.Workspaces[1:] {
		w.Worktree = &api.WorkspaceWorktreeInfo{RepoKey: "repo", IsLinkedWorktree: true}
	}
	s.Workspaces[0].GroupCollapsed = true
	for active, expected := range map[int][]int{0: {0}, 1: {0, 1}, 2: {0, 2}} {
		s.Active = active
		if got := s.VisibleWorkspaceOrder(); !reflect.DeepEqual(got, expected) {
			t.Fatalf("active %d: got %v want %v", active, got, expected)
		}
	}
}
