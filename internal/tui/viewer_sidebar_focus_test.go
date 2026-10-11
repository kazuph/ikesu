package tui

import (
	"bytes"
	"testing"

	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestViewerSessionFocusRevealsWorkspaceInEachSidebar(t *testing.T) {
	st := model.New(t.TempDir(), 120, 40)
	for index := 0; index < 20; index++ {
		st.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	}
	app := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	app.SetServerUI()
	first := app.NewViewer(&bytes.Buffer{}, 80, 24, nil)
	second := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	for _, target := range []int{20, 0, 12} {
		st.Apply(model.CmdSelectWorkspace{Index: target})
		for _, viewer := range []*Viewer{first, second} {
			viewer.FollowSessionFocus(app)
			viewer.with(app, func(ui *App) {
				view := ui.sidebarView()
				for _, pos := range view.Rows {
					if pos.Row.Kind == render.RowWorkspace && pos.Row.Ws == target {
						if pos.Y < view.Workspace.Rect.Y || pos.Y >= view.Workspace.Rect.Y+view.Workspace.Rect.H {
							t.Fatalf("workspace %d is outside viewer %dx%d sidebar", target, viewer.cols, viewer.rows)
						}
						return
					}
				}
				t.Fatalf("workspace %d not visible in viewer %dx%d, scroll=%d", target, viewer.cols, viewer.rows, ui.sidebarScroll)
			})
		}
	}
}
