package render

import (
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/model"
)

func TestStage4SingleNamedTabAndBalancedLocation(t *testing.T) {
	s := model.New("/tmp/parity", 160, 45)
	s.SidebarVisible = false
	s.SidebarHidden = true
	s.Workspaces[0].CustomName = true
	s.Workspaces[0].Name = "parity"
	in := Input{State: s, ServerUI: true, Panes: map[int]*PaneSnap{1: {Agent: "codex", Status: StatusGlyph{Rune: "○"}}}}
	for _, tc := range []struct {
		workspace, tab string
		width          int
		want           string
	}{
		{"parity", "", 26, "parity"},
		{"parity", "NAMED_TAB", 26, "parity · NAMED_TAB"},
		{"WORKSPACE_LONG_ABCDEFGHIJKLMNOPQRSTUVWXYZ", "TAB_LONG_ABCDEFGHIJKLMNOPQRSTUVWXYZ", 26, "WORKSPAC… · TAB_LONG…"},
		{"WORKSPACE_LONG_ABCDEFGHIJKLMNOPQRSTUVWXYZ", "TAB_LONG_ABCDEFGHIJKLMNOPQRSTUVWXYZ", 72, "WORKSPACE_LONG_ABCDEFGHIJKLMNOP… · TAB_LONG_ABCDEFGHIJKLMNOPQRSTUV…"},
	} {
		s.Workspaces[0].Name = tc.workspace
		s.Workspaces[0].Tabs[0].Name = tc.tab
		s.RightSidebarSize = tc.width
		out := Compose(in)
		var text strings.Builder
		for x := 0; x < tc.width-2; x++ {
			text.WriteString(out.Frame.Cell(s.RightSidebarArea().X+1+x, 2).Content)
		}
		if got := strings.TrimSpace(text.String()); got != tc.want || out.Frame.Cell(s.Cols-1, 2).Content != "○" {
			t.Fatalf("width %d tab %q: got %q want %q", tc.width, tc.tab, got, tc.want)
		}
	}
}
