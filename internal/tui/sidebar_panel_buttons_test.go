package tui

import (
	"fmt"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestSidebarPanelDensityHeaderPersists(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "IKESU_CONFIG", t.TempDir()+"/config.toml")
	a := specRuntimeApp(t)
	a.serverUI = true
	a.setSidebarConfig(config.Config{}, true)
	for _, want := range []string{"slim", "full"} {
		frame := render.Compose(a.rightSidebarInput()).Frame
		x := a.st.SidebarArea().W - 7
		for i, ch := range "[" + a.st.WorkspaceDensity + "]" {
			if frame.Cell(x+i, 0).Content != string(ch) {
				t.Fatalf("density button missing: cell %d = %q", x+i, frame.Cell(x+i, 0).Content)
			}
		}
		a.click(uv.Mouse{X: x, Y: 0, Button: uv.MouseLeft})
		c, err := config.Load()
		if err != nil || c.WorkspaceDensity() != want || a.st.WorkspaceDensity != want {
			t.Fatalf("click did not persist %s: config=%s state=%s err=%v", want, c.WorkspaceDensity(), a.st.WorkspaceDensity, err)
		}
	}
}

func TestSidebarPanelPriorityClickReordersEqualStatusImmediately(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "IKESU_CONFIG", t.TempDir()+"/config.toml")
	a := specRuntimeApp(t)
	a.serverUI = true
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdNewWorkspace{Cwd: a.cwd, NoFocus: true})
	a.SetReported(1, "codex", "working")
	a.SetReported(2, "codex", "working")
	a.st.RightSidebarSize = 26
	for _, want := range [][]int{{2, 1}, {1, 2}, {2, 1}} {
		h := render.RightSidebarHeaderFor(a.st, 0)
		a.click(uv.Mouse{X: h.Sort.X, Y: h.Sort.Y, Button: uv.MouseLeft})
		var got []int
		for _, p := range a.sidebarView().Rows {
			if p.Right && p.Row.Kind == render.RowAgent && p.Row.Line == 0 {
				got = append(got, p.Row.Pane)
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("priority/group click order=%v want=%v; scope=%s sort=%s", got, want, a.st.AgentScope, a.sidebarConfig.UI.AgentPanelSort)
		}
		c, err := config.Load()
		if err != nil || c.AgentScope() != a.st.AgentScope || c.UI.AgentPanelSort != a.sidebarConfig.UI.AgentPanelSort {
			t.Fatalf("saved sort differs from displayed sort: %+v %v", c.UI, err)
		}
	}
}

func TestSidebarPanelJobsHasNoAgentSortButtonOrAction(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "IKESU_CONFIG", t.TempDir()+"/config.toml")
	a := specRuntimeApp(t)
	a.serverUI = true
	a.jobsPanel = true
	a.st.RightSidebarSize = 26
	h := render.RightSidebarHeaderFor(a.st, 0)
	frame := render.Compose(a.rightSidebarInput()).Frame
	for x := h.Sort.X; x < h.Sort.X+h.Sort.W; x++ {
		if frame.Cell(x, h.Sort.Y).Content != " " {
			t.Fatal("agent sort button still shown for jobs", frame.Cell(x, h.Sort.Y).Content)
		}
	}
	old := a.st.AgentScope
	a.click(uv.Mouse{X: h.Sort.X, Y: h.Sort.Y, Button: uv.MouseLeft})
	if a.st.AgentScope != old || !a.jobsPanel {
		t.Fatal("invisible sort action changed jobs state")
	}
}
