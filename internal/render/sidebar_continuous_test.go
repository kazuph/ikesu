package render

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/kazuph/goterrace/internal/model"
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
	for _, server := range []bool{false, true} {
		for _, jobs := range []bool{false, true} {
			t.Run(fmt.Sprintf("server=%t/jobs=%t", server, jobs), func(t *testing.T) {
				in := continuousInput(t, server, jobs)
				continuousWidth(in, 26)
				expected := SidebarViewForInput(in).Rows
				for _, w := range []int{26, 18, 17, 10, 5, 3, 2} {
					continuousWidth(in, w)
					f := Compose(in).Frame
					if !reflect.DeepEqual(SidebarViewForInput(in).Rows, expected) {
						t.Fatalf("width %d changes rows", w)
					}
					for _, pos := range expected {
						r := pos.Row
						glyph := ""
						switch r.Kind {
						case RowWorkspace:
							glyph = wsGlyph(in.State, r.Ws, in).Rune
						case RowAgent:
							if r.Line == 0 {
								glyph = in.Panes[r.Pane].Status.Rune
							}
						case RowJob:
							glyph = "●"
						}
						if glyph == " " {
							glyph = "·"
						}
						x := w - 2
						if pos.Right {
							x = in.State.Cols - 1
						}
						got := f.Cell(x, pos.Y).Content
						if glyph != "" && got != glyph {
							t.Fatalf("width %d right=%t row=%+v y=%d status=%q want=%q", w, pos.Right, r, pos.Y, got, glyph)
						}
						if w == 2 && glyph == "" && got != " " {
							t.Fatalf("non-status row at width2 must be blank: %+v y=%d %q", r, pos.Y, got)
						}
					}
				}
			})
		}
	}
}
func TestMachineSidebarContinuousRowsAtEveryWidth(t *testing.T) {
	for _, jobs := range []bool{false, true} {
		in := continuousInput(t, true, jobs)
		m := MachineSidebar{Groups: []MachineGroup{{ID: "local", Label: "LOCAL", Input: &in}, {ID: "remote", Label: "REMOTE", Input: &in}}, Active: "local"}
		type row struct {
			Machine            string
			Row                SidebarRow
			Y                  int
			Right, Header, New bool
		}
		var expected []row
		for _, w := range []int{26, 18, 17, 10, 5, 3, 2} {
			continuousWidth(in, w)
			ComposeMachines(in, &m)
			var got []row
			for _, hit := range m.Hits {
				got = append(got, row{hit.Machine, hit.Row, hit.Rect.Y, hit.Right, hit.Header, hit.New})
			}
			if w == 26 {
				expected = got
			} else if !reflect.DeepEqual(got, expected) {
				t.Fatalf("jobs=%t width=%d changes machine targets/rows\ngot=%+v\nwant=%+v", jobs, w, got, expected)
			}
		}
	}
}
