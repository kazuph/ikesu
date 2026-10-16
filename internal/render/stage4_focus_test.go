package render

import (
	"reflect"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
)

func TestStage4AgentHighlightFollowsFocusedPaneOnly(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	s.Apply(model.CmdSplitFocused{Dir: 0})
	s.Apply(model.CmdNewTab{Workspace: 0})
	s.Workspaces[0].ActiveTab = 0
	s.AgentScope = "sort"
	panes := map[int]*PaneSnap{}
	for id := 1; id <= 3; id++ {
		panes[id] = &PaneSnap{Agent: "codex", Status: StatusGlyph{Key: "idle", Rune: "○"}, StateChangeSeq: uint64(id)}
	}
	for _, custom := range []bool{false, true} {
		cfg := config.Config{}
		cfg.UI.AgentPanelSort = "priority"
		cfg.UI.TransparentSidebarCards = true
		if custom {
			cfg.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "workspace"}}, {{Token: "agent"}}}
		}
		for _, focused := range []int{1, 2} {
			s.Apply(model.CmdFocusPane{Pane: focused})
			in := Input{State: s, ServerUI: true, SidebarConfig: cfg, Panes: panes}
			out := Compose(in)
			count := 0
			for _, pos := range SidebarViewForInput(in).Rows {
				if pos.Row.Kind != RowAgent || pos.Row.Line != 0 {
					continue
				}
				count++
				x := 3
				if custom {
					x = 1
				}
				want := out.Frame.Palette.Subtle
				if pos.Row.Pane == focused {
					want = out.Frame.Palette.Text
				}
				if got := out.Frame.Cell(x, pos.Y).Style.Fg; !reflect.DeepEqual(got, want) {
					t.Fatalf("custom=%t focused=%d card=%d color=%v want=%v", custom, focused, pos.Row.Pane, got, want)
				}
			}
			if count != 3 {
				t.Fatalf("got %d agent cards, want 3", count)
			}
		}
	}
}
