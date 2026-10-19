package tui

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
	"testing"
)

func TestRightSidebarHeaderButtonsNeverOverlapAndBothEdgesClick(t *testing.T) {
	for _, width := range []int{26, 18} {
		for _, scope := range []string{"", "sort"} {
			for _, jobs := range []bool{false, true} {
				newApp := func() *App {
					s := model.New(t.TempDir(), 160, 30)
					s.RightSidebarSize = width
					s.AgentScope = scope
					a := NewApp(s, runtime.NewSupervisor(), &bytes.Buffer{}, "")
					a.jobsPanel = jobs
					return a
				}
				a := newApp()
				h := render.RightSidebarHeaderFor(a.st, 0)
				rects := []layout.Rect{h.Agents, h.Jobs, h.Sort}
				frame := render.Compose(render.Input{State: a.st, JobsPanel: jobs}).Frame
				for i, label := range []string{"[agents]", "[jobs]", render.SidebarAgentSortLabel(a.st)} {
					r := rects[i]
					for offset, ch := range label {
						if frame.Cell(r.X+offset, r.Y).Content != string(ch) {
							t.Fatal("drawn header differs from hit rectangle", width, scope, jobs, label, offset)
						}
					}
				}
				for i, r := range rects {
					if r.X < a.st.RightSidebarArea().X+1 || r.X+r.W > 160 {
						t.Fatal("outside header", r)
					}
					for j, q := range rects {
						if i != j && r.Y == q.Y && max(r.X, q.X) < min(r.X+r.W, q.X+q.W) {
							t.Fatal("overlap", r, q)
						}
					}
				}
				for i, r := range rects {
					for _, x := range []int{r.X, r.X + r.W - 1} {
						a := newApp()
						a.rightSidebarClick(uv.Mouse{X: x, Y: r.Y, Button: uv.MouseLeft})
						if i == 2 {
							if a.st.AgentScope == scope || a.jobsPanel != jobs {
								t.Fatal("sort edge misrouted", width, scope, jobs, x)
							}
						} else if a.jobsPanel != (i == 1) || a.st.AgentScope != scope {
							t.Fatal("tab edge misrouted", width, scope, jobs, x)
						}
					}
				}
			}
		}
	}
}
