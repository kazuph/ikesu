package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
)

func TestWorkspaceNumberFollowsVisibleOrder(t *testing.T) {
	s := model.New("/tmp", 100, 30)
	for i := 0; i < 3; i++ {
		s.Apply(model.CmdNewWorkspace{})
	}
	for i, section := range []model.Section{model.SectionWork, model.SectionNone, model.SectionFavorite, model.SectionPersonal} {
		s.Apply(model.CmdSetSection{Index: i, Section: section})
	}
	check := func() {
		t.Helper()
		order := s.VisibleWorkspaceOrder()
		for i := range s.Workspaces {
			want := ""
			for rank, index := range order {
				if i == index {
					want = fmt.Sprint(rank + 1)
					break
				}
			}
			f := NewFrame(60, 1)
			drawConfiguredSpace(f, s, Input{State: s, WorkspaceNames: map[int]string{i: "NAME"}}, SidebarRow{Kind: RowWorkspace, Ws: i}, []config.SidebarToken{{Token: "workspace"}}, 0, 0, 60)
			got := strings.TrimSpace(frameLine(f, 0))
			got = strings.TrimSpace(strings.TrimPrefix(got, "▌"))
			expected := strings.TrimSpace(want + " NAME")
			if got != expected {
				t.Fatalf("workspace %d: got %q want %q, order %v", i, got, expected, order)
			}
		}
	}
	check()
	s.Apply(model.CmdToggleSection{Section: model.SectionPersonal})
	check()
	s.Apply(model.CmdMoveWorkspace{Index: 0, InsertIndex: 3})
	check()
	s.Apply(model.CmdCloseWorkspace{Index: 1})
	check()
	s.Workspaces[0].Worktree = &api.WorkspaceWorktreeInfo{RepoKey: "repo"}
	s.Workspaces[0].GroupCollapsed = true
	s.Workspaces[1].Worktree = &api.WorkspaceWorktreeInfo{RepoKey: "repo", IsLinkedWorktree: true}
	s.Active = 0
	check()
}

func TestWorkspaceNumberDoesNotChangeWhenScrolled(t *testing.T) {
	s := model.New("/tmp", 100, 24)
	for i := 0; i < 11; i++ {
		s.Apply(model.CmdNewWorkspace{Cwd: "/tmp", NoFocus: true})
	}
	names := map[int]string{}
	for i := range s.Workspaces {
		names[i] = fmt.Sprintf("name%d", i)
	}
	cfg := config.Config{}
	cfg.UI.Sidebar.Spaces.Rows = [][]config.SidebarToken{{{Token: "workspace"}}}
	seen := map[int]bool{}
	for _, offset := range []int{0, 3, 8, 100} {
		in := Input{State: s, WorkspaceNames: names, SidebarConfig: cfg, SidebarScroll: offset}
		frame := Compose(in).Frame
		for _, row := range SidebarViewForInput(in).Rows {
			if row.Right || row.Row.Kind != RowWorkspace {
				continue
			}
			index := row.Row.Ws
			want := fmt.Sprintf("%d name%d", index+1, index)
			if line := frameLine(frame, row.Y); !strings.Contains(line, want) {
				t.Fatalf("offset %d row %d: %q, want %q", offset, index, line, want)
			}
			seen[index] = true
		}
	}
	if len(seen) < 4 {
		t.Fatalf("scroll did not expose enough workspace rows: %v", seen)
	}
}
