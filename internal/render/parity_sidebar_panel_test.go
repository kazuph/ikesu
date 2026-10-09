package render

import (
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
)

func TestParitySidebarPanelDragObservedResizePositions(t *testing.T) {
	// Captures after dragging the local divider from row 23 to row 13 at 45 rows.
	split := float64(13) / 45
	for _, tc := range []struct{ height, agentsY int }{
		{25, 8}, {35, 11}, {43, 13}, {45, 14}, {47, 15}, {55, 17}, {65, 20},
		{86, 26}, {90, 27}, {92, 28}, {100, 30}, {120, 36}, {137, 41}, {440, 128}, {880, 255},
	} {
		state := model.New("/tmp", 160, tc.height)
		state.SidebarSectionSplit = &split
		view := SidebarViewFor(state, nil, config.Config{}, 0, 0)
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
			view := SidebarViewFor(state, nil, config.Config{}, 0, 0)
			if got := view.Agents.Rect.Y - 1; got != edge.y {
				t.Fatalf("height %d split %g: header %d want %d", tc.height, edge.split, got, edge.y)
			}
		}
	}
}
