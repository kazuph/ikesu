package render

import (
	"reflect"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
)

func TestStage4CollapsedGroupKeepsSelectedAndActiveChildren(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	s.Workspaces[0].Worktree = &api.WorkspaceWorktreeInfo{RepoKey: "repo"}
	s.Workspaces[0].GroupCollapsed = true
	for i := 0; i < 2; i++ {
		s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
		s.Workspaces[i+1].Worktree = &api.WorkspaceWorktreeInfo{RepoKey: "repo", IsLinkedWorktree: true}
	}
	selected := 1
	for _, tc := range []struct {
		active   int
		selected *int
		want     []int
	}{{0, nil, []int{0}}, {0, &selected, []int{0, 1}}, {2, &selected, []int{0, 1, 2}}} {
		s.Active = tc.active
		view := SidebarViewForInput(Input{State: s, ServerUI: true, SelectedWorkspace: tc.selected})
		var got []int
		for _, pos := range view.Rows {
			if pos.Row.Kind == RowWorkspace {
				got = append(got, pos.Row.Ws)
			}
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("active=%d selected=%v: got %v want %v", tc.active, tc.selected, got, tc.want)
		}
	}
}
