package render

import (
	"image/color"
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
)

func TestConfigCustomColorsChangeRenderedCells(t *testing.T) {
	keys := []string{"accent", "panel_bg", "surface_dim", "surface0", "surface1", "overlay0", "overlay1", "text", "subtext0", "mauve", "green", "yellow", "red", "teal", "peach"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			c, err := config.Decode([]byte("[theme.custom]\n" + key + "='#123456'\n"))
			if err != nil || len(c.Warnings) > 0 {
				t.Fatal(c.Warnings, err)
			}
			s := model.New("/tmp/config-colors", 160, 45)
			s.Apply(model.CmdNewWorkspace{Cwd: "/tmp/second"})
			selected := 0
			scenes := []Input{
				{State: s, ServerUI: true, SelectedWorkspace: &selected, Git: map[int]GitStatus{0: {Branch: "branch", Ahead: 2, Behind: 1, Added: 3, Deleted: 4}}},
				{State: s, Git: map[int]GitStatus{0: {Branch: "branch", Ahead: 2, Behind: 1, Added: 3, Deleted: 4}}},
				{State: s, Help: &HelpInput{Rows: []HelpRow{{Bind: "prefix+?", Cmd: "show help"}}}},
				{State: s, Navigator: &Navigator{Filter: "done", Total: 2, Rows: []NavigatorRow{{Name: "pane", WorkspaceName: "ws", Agent: "codex", Status: "done"}, {Name: "other", Status: "working"}}}},
				{State: s, Navigator: &Navigator{Filter: "working", Total: 1, Rows: []NavigatorRow{{Name: "working", Status: "working"}}}},
				{State: s, Toast: &Toast{Title: "alert", Kind: "needs_attention", Body: "details"}},
				{State: s, JobsPanel: true, Jobs: []JobRow{{Label: "job", Status: "run", Color: Peach, CallerPane: "p1"}}},
				{State: s, Settings: &Settings{Theme: "catppuccin"}},
			}
			want := color.RGBA{18, 52, 86, 255}
			changed := 0
			for _, in := range scenes {
				base := Compose(in).Frame
				in.SidebarConfig = c
				after := Compose(in).Frame
				for i, cell := range after.Cells {
					if cell.Style.Fg == want && base.Cells[i].Style.Fg != want || cell.Style.Bg == want && base.Cells[i].Style.Bg != want {
						changed++
					}
				}
			}
			if changed == 0 {
				t.Fatal("configured color never reached a rendered cell", key)
			}
		})
	}
}
