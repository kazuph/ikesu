package render

import (
	"fmt"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"testing"
)

func TestNarrowSidebarJapaneseEncoderMatchesEveryCell(t *testing.T) {
	for _, server := range []bool{false, true} {
		for _, custom := range []bool{false, true} {
			for _, machine := range []bool{false, true} {
				for width := 2; width < model.SidebarMinDefault; width++ {
					t.Run(fmt.Sprintf("server=%t/custom=%t/machine=%t/width=%d", server, custom, machine, width), func(t *testing.T) {
						s := model.New("/tmp/sidebar-wide-crop", 240, 44)
						s.SidebarVisible, s.SidebarSize, s.RightSidebarSize, s.RightSidebarMode = false, width, width, "compact"
						s.Workspaces[0].CustomName = true
						in := Input{State: s, ServerUI: server, Panes: map[int]*PaneSnap{1: {Agent: "codex", Title: "日本語作業", Status: StatusGlyph{Rune: "●"}}}}
						if custom {
							in.SidebarConfig.UI.Sidebar.Spaces.Rows = [][]config.SidebarToken{{{Token: "workspace"}}}
							in.SidebarConfig.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "workspace"}}}
						}
						encoder := &Encoder{}
						terminal := vt.NewEmulator(s.Cols, s.Rows)
						defer terminal.Close()
						for _, name := range []string{"日本語作業", "中文調査"} {
							s.Workspaces[0].Name = name
							var f *Frame
							if machine {
								m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Label: "日本語", Input: &in}, {ID: "remote", Label: "別端末", Input: &in}}}
								f = ComposeMachines(in, &m).Frame
							} else {
								f = Compose(in).Frame
							}
							if _, err := terminal.Write(encoder.Frame(f, 0, 0, false)); err != nil {
								t.Fatal(err)
							}
							for y := 0; y < f.H; y++ {
								for x := 0; x < f.W; x++ {
									want, got := f.Cell(x, y), terminal.CellAt(x, y)
									if want.Width == 2 && (x+1 >= f.W || f.Cell(x+1, y).Width != 0) {
										t.Fatalf("split wide character at (%d,%d): %+v", x, y, want)
									}
									if got.Content != want.Content || got.Width != want.Width || !got.Style.Equal(&want.Style) {
										t.Fatalf("name=%s cell(%d,%d) decoded=%+v frame=%+v", name, x, y, got, want)
									}
								}
							}
						}
					})
				}
			}
		}
	}
}
