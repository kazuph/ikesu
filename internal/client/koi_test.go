package client

import (
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
	"os"
	"testing"
)

func TestKoiClockStopsWhenOffEmptyHiddenOrFull(t *testing.T) {
	for _, condition := range []string{"visible", "off", "empty", "hidden", "full", "blurred"} {
		state := model.New(t.TempDir(), 160, 45)
		state.SidebarVisible = true
		state.SidebarSize = 26
		state.RightSidebarSize = 30
		panes := map[int]*render.PaneSnap{1: {Agent: "codex", Status: render.StatusGlyph{Key: "idle"}}}
		var cfg config.Config
		switch condition {
		case "off":
			off := false
			cfg.UI.Koi = &off
		case "empty":
			panes = nil
		case "hidden":
			state.SidebarHidden = true
			state.SidebarVisible = false
			state.RightSidebarMode = "hidden"
		case "full":
			for i := 0; i < 60; i++ {
				cfg.UI.Sidebar.Agents.Rows = append(cfg.UI.Sidebar.Agents.Rows, []config.SidebarToken{{Token: "agent"}})
			}
		}
		snapshot := render.Input{State: state, Panes: panes, SidebarConfig: cfg}
		file, err := os.CreateTemp(t.TempDir(), "output")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		m := &machineClient{active: "local", order: []string{"local"}, out: file, size: tui.Winsize{Cols: 160, Rows: 45}, settings: &localSettings{config: &cfg}, endpoints: map[string]*managedEndpoint{"local": {snapshot: &snapshot, profile: machine.Profile{ID: "local", Enabled: true}}}}
		m.blurred = condition == "blurred"
		if err := m.paint(); err != nil {
			t.Fatal(err)
		}
		want := condition == "visible"
		if got := !m.koiNext.IsZero(); got != want {
			t.Fatalf("%s clock=%t want=%t", condition, got, want)
		}
		if snapshot.Koi || snapshot.KoiTime != 0 {
			t.Fatal("server snapshot mutated")
		}
	}
}
