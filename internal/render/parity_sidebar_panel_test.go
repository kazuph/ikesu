package render

import (
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
)

func TestParitySidebarSmallSettledGeometry(t *testing.T) {
	for _, tc := range []struct{ height, split int }{
		{7, 4}, {8, 5}, {9, 6}, {10, 7}, {11, 7}, {12, 7}, {13, 7}, {14, 7}, {16, 8},
	} {
		for _, server := range []bool{false, true} {
			s := model.New(t.TempDir(), 160, tc.height)
			if SidebarSplitY(s) != tc.split || SidebarFooter(s).Y != tc.split-1 {
				t.Fatalf("height %d: divider/footer disagree with settled capture", tc.height)
			}
			v := SidebarViewFor(s, nil, config.Config{}, 0, 0, server)
			for _, p := range v.Rows {
				if tc.height == 7 && (p.Row.Kind == RowSection || server && p.Y == 1) {
					t.Fatalf("height 7 server=%t: clipped header remains at %d", server, p.Y)
				}
			}
		}
	}
}

func TestParityLocalShortSidebarKeepsPartialCardAwayFromFooter(t *testing.T) {
	for _, density := range []string{"full", "slim"} {
		for _, height := range []int{8, 9, 10} {
			s := model.New(t.TempDir(), 160, height)
			s.WorkspaceDensity = density
			v := SidebarViewFor(s, nil, config.Config{}, 0, 0, false)
			clipped := height < 10 || density == "full"
			if clipped && v.Workspace.Total > v.Workspace.Rect.H {
				t.Fatalf("%s height %d: clipped first card leaves a scrollbar", density, height)
			}
			for _, p := range v.Rows {
				if clipped && p.Y == v.Workspace.Rect.Y+v.Workspace.Rect.H-1 && (p.Row.Kind == RowWorkspace || p.Row.Kind == RowGit) {
					t.Fatalf("%s height %d: partial card reaches the footer", density, height)
				}
			}
		}
	}
}

func TestParitySidebarPanelDragObservedResizePositions(t *testing.T) {
	// Captures after dragging the local divider from row 23 to row 13 at 45 rows.
	split := float64(13) / 45
	for _, tc := range []struct{ height, agentsY int }{
		{25, 8}, {35, 11}, {43, 13}, {45, 14}, {47, 15}, {55, 17}, {65, 20},
		{86, 26}, {90, 27}, {92, 28}, {100, 30}, {120, 36}, {137, 41}, {440, 128}, {880, 255},
	} {
		state := model.New("/tmp", 160, tc.height)
		state.SidebarSectionSplit = &split
		view := SidebarViewFor(state, nil, config.Config{}, 0, 0, false)
		if view.Agents.Rect.Y-1 != tc.agentsY {
			t.Fatalf("height %d: agents header %d want %d", tc.height, view.Agents.Rect.Y-1, tc.agentsY)
		}
		if SidebarFooter(state).Y != tc.agentsY-2 {
			t.Fatalf("height %d: footer must remain above divider", tc.height)
		}
	}
}

func TestParitySidebarPanelDraggedEdgeCaptures(t *testing.T) {
	for _, tc := range []struct {
		height, top, bottom int
	}{
		{25, 8, 23}, {35, 8, 33}, {45, 8, 42}, {90, 10, 82}, {120, 13, 109},
	} {
		for _, edge := range []struct {
			split float64
			y     int
		}{{.1, tc.top}, {.9, tc.bottom}} {
			state := model.New("/tmp", 160, tc.height)
			state.SidebarSectionSplit = &edge.split
			view := SidebarViewFor(state, nil, config.Config{}, 0, 0, false)
			if got := view.Agents.Rect.Y - 1; got != edge.y {
				t.Fatalf("height %d split %g: header %d want %d", tc.height, edge.split, got, edge.y)
			}
		}
	}
}
