package tui

import (
	"fmt"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestSlimGitRowClickRightClickAndDragPreserveWorkspaceIdentity(t *testing.T) {
	teststate.Lock(t)
	for _, endpoint := range []bool{false, true} {
		t.Run(fmt.Sprint(endpoint), func(t *testing.T) {
			a, _ := worktreeApp(t)
			if endpoint {
				a.SetServerUI()
			}
			var cfg config.Config
			cfg.UI.WorkspacePanelDensity = "slim"
			a.SetSidebarConfig(cfg, false)
			a.newWorkspace()
			first, second := a.st.Workspaces[0].ID, a.st.Workspaces[1].ID
			a.workspaceNames = map[int]string{0: "first", 1: "second"}
			a.workspaceGit = map[int]render.GitStatus{0: {Branch: "first"}, 1: {Branch: "second"}}
			y0, y1 := sidebarCardY(a, 0), sidebarCardY(a, 1)
			click := func(y int) {
				sidebarEvent(a, input.KindMouseClick, 3, y)
				sidebarEvent(a, input.KindMouseRelease, 3, y)
			}
			click(y0 + 1)
			if a.st.Active != 0 || a.menu != nil {
				t.Fatal("inactive Git click must select without opening a menu")
			}
			click(y0 + 1)
			if endpoint && a.menu != nil || !endpoint && (a.menu == nil || a.menu.ws != 0) {
				t.Fatal("active Git click does not match local/endpoint behavior", a.menu)
			}
			a.menu = nil
			for ws, y := range []int{y0, y1} {
				a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: 3, Y: y + 1, Button: uv.MouseRight}})
				if a.menu == nil || a.menu.ws != ws || a.st.Active != 0 {
					t.Fatal("Git right click must target its workspace without selecting it", ws, a.menu)
				}
				a.menu = nil
			}
			sidebarEvent(a, input.KindMouseClick, 3, y1+1)
			sidebarEvent(a, input.KindMouseMotion, 3, y0)
			if a.sidebarGesture == nil || !a.sidebarGesture.moved || a.sidebarGesture.ws != 1 {
				t.Fatal("Git row did not start workspace drag")
			}
			sidebarEvent(a, input.KindMouseRelease, 3, y0)
			wantActive := 1
			if endpoint {
				wantActive = 0
			}
			if a.st.Workspaces[0].ID != second || a.st.Workspaces[1].ID != first || a.st.Active != wantActive {
				t.Fatal("drag reordered or selected the wrong workspace", a.st.Active)
			}
			if a.workspaceNames[0] != "second" || a.workspaceNames[1] != "first" || a.workspaceGit[0].Branch != "second" || a.workspaceGit[1].Branch != "first" {
				t.Fatal("drag left display metadata attached to old positions")
			}
		})
	}
}

func TestSlimNewWorkspaceAndGitRowWheelKeepSectionHeaders(t *testing.T) {
	teststate.Lock(t)
	for _, endpoint := range []bool{false, true} {
		t.Run(fmt.Sprint(endpoint), func(t *testing.T) {
			a, _ := worktreeApp(t)
			if endpoint {
				a.SetServerUI()
			}
			var cfg config.Config
			cfg.UI.WorkspacePanelDensity = "slim"
			a.SetSidebarConfig(cfg, false)
			a.exec(model.CmdResize{Cols: 160, Rows: 25})
			for i := 0; i < 12; i++ {
				a.newWorkspace()
			}
			before := a.sidebarScroll
			if before == 0 {
				t.Fatal("new workspace remained outside the visible sidebar")
			}
			y := -1
			for _, pos := range a.sidebarView().Rows {
				if pos.Row.Kind == render.RowGit {
					y = pos.Y
					break
				}
			}
			if y < 0 {
				t.Fatal("visible Git row missing")
			}
			a.wheel(uv.Mouse{X: 3, Y: y, Button: uv.MouseWheelUp})
			if a.sidebarScroll != before-2 {
				t.Fatal("slim Git wheel must move one two-row card", before, a.sidebarScroll)
			}
			a.wheel(uv.Mouse{X: 3, Y: y, Button: uv.MouseWheelDown})
			if a.sidebarScroll != before {
				t.Fatal("opposite wheel did not return to the same offset")
			}
			if rows := a.sidebarView().Rows; len(rows) < 4 || rows[2].Row.Kind != render.RowSection || rows[3].Row.Kind != render.RowRule {
				t.Fatal("wheel scrolled away the section header")
			}
		})
	}
}
