package render

import (
	"reflect"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
)

func TestPriorityAgentSortUsesLatestStateWithinBucket(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	s.Apply(model.CmdSplitFocused{Dir: 0})
	s.AgentScope = "sort"
	panes := map[int]*PaneSnap{
		1: {Agent: "codex", Status: StatusGlyph{Key: "idle"}, StateChangeSeq: 1},
		2: {Agent: "codex", Status: StatusGlyph{Key: "idle"}, StateChangeSeq: 2},
	}
	cfg := config.Config{}
	for _, sort := range []string{"priority", "spaces"} {
		cfg.UI.AgentPanelSort = sort
		var got []int
		for _, row := range SidebarRows(s, panes, cfg) {
			if row.Kind == RowAgent && row.Line == 0 {
				got = append(got, row.Pane)
			}
		}
		want := []int{1, 2}
		if sort == "priority" {
			want = []int{2, 1}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: %v, want %v", sort, got, want)
		}
	}
}
