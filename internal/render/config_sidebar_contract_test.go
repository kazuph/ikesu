package render

import (
	"reflect"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func TestConfigSidebarRowsAndGapsChangeRenderedCells(t *testing.T) {
	for _, tc := range []struct{ key, first, second string }{
		{"agents.rows", "[ui.sidebar.agents]\nrows=[['workspace'],['agent']]", "[ui.sidebar.agents]\nrows=[['pane'],['tab']]"},
		{"agents.rows_by_agent", "[ui.sidebar.agents.rows_by_agent]\ncodex=[['agent']]", "[ui.sidebar.agents.rows_by_agent]\ncodex=[['workspace']]"},
		{"agents.row_gap", "[ui.sidebar.agents]\nrow_gap=0", "[ui.sidebar.agents]\nrow_gap=1"},
		{"spaces.rows", "[ui.sidebar.spaces]\nrows=[['workspace']]", "[ui.sidebar.spaces]\nrows=[['branch']]"},
		{"spaces.row_gap", "[ui.sidebar.spaces]\nrow_gap=0", "[ui.sidebar.spaces]\nrow_gap=1"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			s := model.New("/tmp/project", 160, 45)
			s.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
			s.Apply(model.CmdNewWorkspace{Name: "second", Cwd: "/tmp/second"})
			panes := map[int]*PaneSnap{1: {Agent: "codex", Label: "codex", Status: StatusGlyph{Key: "idle"}}, 2: {Agent: "codex", Label: "codex", Status: StatusGlyph{Key: "idle"}}, 3: {Agent: "claude", Label: "claude", Status: StatusGlyph{Key: "idle"}}}
			var frames []*Frame
			for _, source := range []string{tc.first, tc.second} {
				c, err := config.Decode([]byte(source))
				if err != nil || len(c.Warnings) > 0 {
					t.Fatal(c.Warnings, err)
				}
				frames = append(frames, Compose(Input{State: s, ServerUI: true, Panes: panes, SidebarConfig: c, Git: map[int]GitStatus{0: {Branch: "main"}, 1: {Branch: "branch"}}}).Frame)
			}
			if reflect.DeepEqual(frames[0].Cells, frames[1].Cells) {
				t.Fatal("setting did not change rendered cells")
			}
		})
	}
}

func TestConfigAgentBorderLabelFlagDoesNotChangeRenderedCells(t *testing.T) {
	s := model.New("/tmp/project", 140, 30)
	panes := map[int]*PaneSnap{1: {Agent: "codex", Label: "codex", Status: StatusGlyph{Key: "idle"}}}
	var frames []*Frame
	for _, source := range []string{"[ui]\nshow_agent_labels_on_pane_borders=false", "[ui]\nshow_agent_labels_on_pane_borders=true"} {
		c, err := config.Decode([]byte(source))
		if err != nil || len(c.Warnings) > 0 {
			t.Fatal(c.Warnings, err)
		}
		frames = append(frames, Compose(Input{State: s, ServerUI: true, Panes: panes, SidebarConfig: c}).Frame)
	}
	if !reflect.DeepEqual(frames[0].Cells, frames[1].Cells) {
		t.Fatal("SPEC 1008/1022: flag must not change pane titles")
	}
}

func TestConfigToastPositionsChangeRenderedRects(t *testing.T) {
	s := model.New("/tmp/project", 140, 40)
	for _, section := range []string{"herdr", "clipboard"} {
		t.Run(section, func(t *testing.T) {
			for _, position := range []string{"top-left", "top-right", "bottom-left", "bottom-right"} {
				c, err := config.Decode([]byte("[ui.toast." + section + "]\nposition='" + position + "'\n"))
				if err != nil || len(c.Warnings) > 0 {
					t.Fatal(c.Warnings, err)
				}
				in := Input{State: s, SidebarConfig: c}
				if section == "herdr" {
					in.Toast = &Toast{Title: "notification", Position: c.UI.Toast.Herdr.Position}
				} else {
					in.Copied = "Copied 3 lines"
				}
				out := Compose(in)
				r := out.ToastRect
				if r.W == 0 || r.H == 0 {
					t.Fatal("missing toast", section, position, r)
				}
				if (r.X < s.Cols/2) != (position == "top-left" || position == "bottom-left") || (r.Y < s.Rows/2) != (position == "top-left" || position == "top-right") {
					t.Fatal(section, position, r)
				}
			}
		})
	}
}
