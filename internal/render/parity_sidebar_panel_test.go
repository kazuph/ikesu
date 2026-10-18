package render

import (
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
)

func TestSidebarSmallSettledGeometry(t *testing.T) {
	for _, height := range []int{7, 8, 9, 10, 11, 12, 13, 14, 16} {
		for _, server := range []bool{false, true} {
			state := model.New(t.TempDir(), 160, height)
			if SidebarFooter(state).Y != height-3 {
				t.Fatal("workspace footer must follow full-height sidebar")
			}
			view := SidebarViewFor(state, nil, config.Config{}, 0, 0, server, nil)
			for _, pos := range view.Rows {
				if pos.Right {
					if pos.Y < 0 || pos.Y >= height {
						t.Fatal("right row escaped sidebar")
					}
				} else if pos.Y >= SidebarFooter(state).Y {
					t.Fatal("workspace overlapped fixed footer")
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
			v := SidebarViewFor(s, nil, config.Config{}, 0, 0, false, nil)
			cardRows := 3
			if density == "slim" {
				cardRows = 2
			}
			clipped := v.Workspace.Rect.H < 2+cardRows
			if clipped && v.Workspace.Total > v.Workspace.Rect.H {
				t.Fatalf("%s height %d: clipped first card leaves a scrollbar", density, height)
			}
			for _, p := range v.Rows {
				if !p.Right && clipped && p.Y == v.Workspace.Rect.Y+v.Workspace.Rect.H-1 && (p.Row.Kind == RowWorkspace || p.Row.Kind == RowGit) {
					t.Fatalf("%s height %d: partial card reaches the footer", density, height)
				}
			}
		}
	}
}

func TestSidebarRightPanelUsesFullHeightAfterResize(t *testing.T) {
	for _, height := range []int{25, 35, 43, 45, 47, 55, 65, 86, 90, 92, 100, 120, 137, 440, 880} {
		state := model.New("/tmp", 160, height)
		view := SidebarViewFor(state, nil, config.Config{}, 0, 0, false, nil)
		if view.Agents.Rect.Y != 2 || view.Agents.Rect.H != height-2 {
			t.Fatal("right panel must start beneath its header and fill remaining height", view.Agents)
		}
	}
}

func TestSidebarLegacyPanelSplitHasNoEffect(t *testing.T) {
	for _, height := range []int{25, 35, 45, 90, 120} {
		for _, split := range []float64{.1, .5, .9} {
			state := model.New("/tmp", 160, height)
			state.SidebarSectionSplit = &split
			view := SidebarViewFor(state, nil, config.Config{}, 0, 0, false, nil)
			if view.Agents.Rect.Y != 2 || SidebarFooter(state).Y != height-3 {
				t.Fatal("obsolete split moved independent sidebar")
			}
		}
	}
}
