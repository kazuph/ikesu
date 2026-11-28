package render

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"reflect"
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
)

func continuousInput(t *testing.T, server, jobs bool) Input {
	t.Helper()
	s := model.New(t.TempDir(), 240, 60)
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	s.Workspaces[0].Name = "FIRST_LONG_NAME"
	s.Workspaces[0].CustomName = true
	s.Workspaces[1].Name = "SECOND_LONG_NAME"
	s.Workspaces[1].CustomName = true
	return Input{State: s, ServerUI: server, JobsPanel: jobs, Panes: map[int]*PaneSnap{
		1: {Agent: "codex", Title: "FIRST_PANE", Status: StatusGlyph{Rune: "●", Key: "working"}},
		2: {Agent: "codex", Title: "SECOND_PANE", Status: StatusGlyph{Rune: "!", Key: "blocked"}},
	}, Jobs: []JobRow{{ID: "j1", Label: "FIRST_JOB", Status: "running"}, {ID: "j2", Label: "SECOND_JOB", Status: "exited"}}}
}
func continuousWidth(in Input, w int) {
	s := in.State
	s.SidebarSize = w
	s.RightSidebarSize = w
	s.SidebarVisible = w >= 18
	s.SidebarHidden = false
	s.RightSidebarMode = ""
	if w < 18 {
		s.RightSidebarMode = "compact"
	}
}
func TestSidebarContinuousRowsAndStatusAtEveryWidth(t *testing.T) {
	if teststate.RunIsolatedParallel(t) {
		return
	}
	for _, server := range []bool{false, true} {
		for _, jobs := range []bool{false, true} {
			for _, custom := range []bool{false, true} {
				for _, machine := range []bool{false, true} {
					in := continuousInput(t, server, jobs)
					if custom {
						in.SidebarConfig.UI.Sidebar.Spaces.Rows = [][]config.SidebarToken{{{Token: "workspace"}}}
						in.SidebarConfig.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "workspace"}}}
					}
					draw := func() (*Frame, *MachineSidebar) {
						if machine {
							m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Label: "LOCAL", Input: &in}, {ID: "remote", Label: "REMOTE", Input: &in}}}
							f := ComposeMachines(in, &m).Frame
							return f, &m
						}
						return Compose(in).Frame, nil
					}
					continuousWidth(in, 18)
					reference, machines := draw()
					expected := SidebarViewForInput(in).Rows
					statusRows := map[bool]map[int]bool{false: {}, true: {}}
					leftMachineHeaders := map[int]bool{}
					if machine {
						for _, hit := range machines.Hits {
							if hit.Header && !hit.Right {
								leftMachineHeaders[hit.Rect.Y] = true
							}
							if !hit.Header && !hit.IsHeaderButton() {
								statusRows[hit.Right][hit.Rect.Y] = hit.Row.Kind == RowWorkspace || hit.Row.Kind == RowAgent && hit.Row.Line == 0 || hit.Row.Kind == RowJob
							}
						}
					} else {
						for _, pos := range expected {
							statusRows[pos.Right][pos.Y] = pos.Row.Kind == RowWorkspace || pos.Row.Kind == RowAgent && pos.Row.Line == 0 || pos.Row.Kind == RowJob
						}
					}
					for width := 17; width >= 2; width-- {
						continuousWidth(in, width)
						f, actual := draw()
						var actualWidth *Frame
						if machine {
							// Machine buttons occupy the right edge at the actual width.
							actualWidth = NewFrame(f.W, f.H)
							actualWidth.Palette = f.Palette
							actual.drawExpanded(actualWidth, in)
							applyPersistentBackground(actualWidth, in, nil)
						}
						if !reflect.DeepEqual(SidebarViewForInput(in).Rows, expected) {
							t.Fatal("narrow width changed row target/Y/order", width)
						}
						for _, right := range []bool{false, true} {
							x, rx := 0, 0
							if right {
								x = in.State.Cols - width + 1
								rx = in.State.Cols - 18 + 1
							}
							for y := 0; y < in.State.Rows; y++ {
								if !right && y == in.State.Rows-1 {
									// The menu follows the actual width, independently of the cropped sidebar content.
									r := SidebarMenuRect(in.State, server)
									for i, c := range "[menu]" {
										if r.W > 0 && f.Cell(r.X+i, r.Y).Content != string(c) {
											t.Fatalf("width %d: misplaced menu %+v", width, r)
										}
									}
									continue
								}
								skip := 0
								if width == 2 && statusRows[right][y] && (reference.Cell(rx, y).Content == " " || reference.Cell(rx, y).Content == "▌") {
									skip = 1
								}
								for col := 0; col < width-1; col++ {
									if width == 2 && !right && y == 0 && col == 0 {
										// The strip's top cell is the » that restores the sidebar.
										if f.Cell(x, y).Content != "»" {
											t.Fatalf("strip top cell %+v", f.Cell(x, y))
										}
										continue
									}
									want := reference.Cell(rx+skip+col, y)
									if !right && leftMachineHeaders[y] {
										want = actualWidth.Cell(x+col, y)
									}
									if !reflect.DeepEqual(f.Cell(x+col, y), want) {
										t.Fatalf("width=%d server=%t jobs=%t custom=%t machine=%t right=%t x=%d y=%d got=%+v want=%+v", width, server, jobs, custom, machine, right, col, y, f.Cell(x+col, y), want)
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

func TestMachineSidebarContinuousRowsAtEveryWidth(t *testing.T) {
	for _, jobs := range []bool{false, true} {
		in := continuousInput(t, true, jobs)
		m := MachineSidebar{Groups: []MachineGroup{{ID: "local", Label: "LOCAL", Input: &in}, {ID: "remote", Label: "REMOTE", Input: &in}}, Active: "local"}
		type row struct {
			Machine       string
			Row           SidebarRow
			Y             int
			Right, Header bool
		}
		var expected []row
		for _, w := range []int{26, 18, 17, 10, 5, 3, 2} {
			continuousWidth(in, w)
			ComposeMachines(in, &m)
			var got []row
			for _, hit := range m.Hits {
				if hit.IsHeaderButton() {
					continue
				}
				got = append(got, row{hit.Machine, hit.Row, hit.Rect.Y, hit.Right, hit.Header})
			}
			if w == 26 {
				expected = got
			} else if !reflect.DeepEqual(got, expected) {
				t.Fatalf("jobs=%t width=%d changes machine targets/rows\ngot=%+v\nwant=%+v", jobs, w, got, expected)
			}
		}
	}
}
