package render

import (
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
)

func TestStage4GitTokensSeparateCountsAndHideChildOrConnected(t *testing.T) {
	cfg, err := config.Decode([]byte("[ui.sidebar.spaces]\nrows=[[\"git_status\"],[\"git_diff\"]]\n"))
	if err != nil || len(cfg.Warnings) != 0 {
		t.Fatalf("config: %v %v", err, cfg.Warnings)
	}
	for _, tc := range []struct {
		name         string
		git          GitStatus
		status, diff string
	}{
		{"ahead and changes", GitStatus{Ahead: 1, Added: 2, Deleted: 1}, " ↑1", "   +2 -1"},
		{"behind and changes", GitStatus{Behind: 1, Added: 3, Deleted: 1}, " ↓1", "   +3 -1"},
		{"all counts", GitStatus{Ahead: 2, Behind: 3, Added: 4, Deleted: 5}, " ↑2 ↓3", "   +4 -5"},
		{"zero counts", GitStatus{}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, connected := range []bool{false, true} {
				for _, child := range []bool{false, true} {
					s := model.New("/tmp/git-fixture", 160, 45)
					if child {
						s.Workspaces[0].Worktree = &api.WorkspaceWorktreeInfo{IsLinkedWorktree: true}
					}
					in := Input{State: s, ServerUI: connected, SidebarConfig: cfg, Git: map[int]GitStatus{0: tc.git}}
					f := NewFrame(25, 2)
					drawConfiguredSpace(f, s, in, SidebarRow{Kind: RowWorkspace, Ws: 0}, cfg.UI.Sidebar.Spaces.Rows[0], 0, 0, 25)
					drawConfiguredSpace(f, s, in, SidebarRow{Kind: RowGit, Ws: 0}, cfg.UI.Sidebar.Spaces.Rows[1], 0, 1, 25)
					for y, want := range []string{tc.status, tc.diff} {
						var row strings.Builder
						for x := 1; x < 25; x++ {
							if c := f.Cell(x, y); c != nil {
								row.WriteString(c.Content)
							}
						}
						want = strings.TrimPrefix(want, " ")
						if connected || child {
							want = ""
						}
						if got := strings.TrimRight(row.String(), " "); got != want {
							t.Fatalf("connected=%v child=%v row=%d got=%q want=%q", connected, child, y, got, want)
						}
					}
				}
			}
		})
	}
}

func TestStage4DefaultConnectedGitShowsBranchWithoutCounts(t *testing.T) {
	s := model.New("/tmp/git-fixture", 160, 45)
	in := Input{State: s, Git: map[int]GitStatus{0: {Branch: "main", Ahead: 1, Added: 2, Deleted: 1}}}
	for _, connected := range []bool{false, true} {
		in.ServerUI = connected
		out := Compose(in)
		rows := SidebarRows(s, nil)
		_, y, _, _ := SidebarContent(s.SidebarArea())
		for i, r := range rows {
			if r.Kind != RowGit {
				continue
			}
			line := frameLine(out.Frame, y+i)[:25]
			if !strings.Contains(line, "main") {
				t.Fatal(line)
			}
			if strings.Contains(line, "↑1") == connected || strings.Contains(line, "+2") == connected {
				t.Fatalf("connected=%v line=%q", connected, line)
			}
		}
	}
}
