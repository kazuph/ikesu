package render

import (
	"encoding/json"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// Both versions run this fixture; the runner compares every cell, color and attribute.
func TestSidebarStatusReferenceCells(t *testing.T) {
	for _, server := range []bool{false, true} {
		for _, jobs := range []bool{false, true} {
			for _, overflow := range []bool{false, true} {
				for _, custom := range []bool{false, true} {
					for _, width := range []int{18, 20, 26, 40, 72} {
						s := model.New("/tmp/sidebar-reference", 240, 44)
						s.SidebarVisible, s.SidebarSize, s.RightSidebarSize = true, width, width
						s.AgentScope = "all"
						n := 3
						if overflow {
							n = 24
						}
						for i := 1; i < n; i++ {
							s.Apply(model.CmdNewWorkspace{Cwd: "/tmp/sidebar-reference", NoFocus: true})
						}
						in := Input{State: s, ServerUI: server, JobsPanel: jobs, Panes: map[int]*PaneSnap{}}
						for i, ws := range s.Workspaces {
							ws.Name, ws.CustomName = fmt.Sprintf("Workspace_%02d_LONG_NAME", i), true
							pid := ws.TabRef().Layout.Focused()
							in.Panes[pid] = &PaneSnap{Agent: "codex", Title: "Task title", Status: StatusGlyph{Rune: "●", Color: color.RGBA{R: 80, G: 180, B: 90, A: 255}, Bold: true, Key: "working"}}
							in.Jobs = append(in.Jobs, JobRow{ID: fmt.Sprintf("j%03d", i), Label: "Job_long_name", Status: "running", Color: color.RGBA{R: 180, A: 255}, Elapsed: "2s"})
						}
						if custom {
							in.SidebarConfig.UI.Sidebar.Spaces.Rows = [][]config.SidebarToken{{{Token: "state_icon"}, {Token: "workspace"}}}
							in.SidebarConfig.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "state_icon"}, {Token: "pane"}, {Token: "workspace"}}}
							in.SidebarConfig.UI.TransparentSidebarCards = true
						}
						for _, machine := range []bool{false, true} {
							key := fmt.Sprintf("server-%t_jobs-%t_overflow-%t_custom-%t_machine-%t_width-%d", server, jobs, overflow, custom, machine, width)
							var f *Frame
							if machine {
								m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Label: "Local", Status: "online", Input: &in}, {ID: "remote", Label: "Remote", Status: "awaiting approval", Reason: "https://example.invalid/approval/full-url?confirm=one", Input: &in}}}
								f = ComposeMachines(in, &m).Frame
							} else {
								f = Compose(in).Frame
							}
							for _, right := range []bool{false, true} {
								x := 0
								if right {
									x = s.Cols - width
								}
								var cells []uv.Cell
								for y := 0; y < s.Rows; y++ {
									for dx := 0; dx < width; dx++ {
										cells = append(cells, *f.Cell(x+dx, y))
									}
								}
								data, err := json.Marshal(cells)
								if err != nil {
									t.Fatal(err)
								}
								if dir := os.Getenv("SIDEBAR_REFERENCE_OUT"); dir != "" {
									if err := os.MkdirAll(dir, 0755); err != nil {
										t.Fatal(err)
									}
									if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s_right-%t.json", key, right)), data, 0644); err != nil {
										t.Fatal(err)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}
