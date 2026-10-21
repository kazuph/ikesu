package tui

import (
	"bytes"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/runtime"
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

func TestClientViewerCardPressSelectsBeforeDragWithoutChangingOtherViewer(t *testing.T) {
	st := model.New(t.TempDir(), 160, 45)
	st.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	app := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	app.SetServerUI()
	first := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	second := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	first.SetSnapshotSink(func(render.Input) error { return nil })
	first.with(app, func(ui *App) {
		for _, pos := range ui.sidebarView().Rows {
			if pos.Row.Kind == render.RowWorkspace && pos.Row.Ws == 1 {
				if !ui.sidebarPress(uv.Mouse{X: 4, Y: pos.Y, Button: uv.MouseLeft}) || ui.st.Active != 1 {
					t.Fatal("card press did not select before drag")
				}
				if ui.sidebarGesture == nil || ui.sidebarGesture.active {
					t.Fatal("gesture lost original inactive click state")
				}
				return
			}
		}
		t.Fatal("second card missing")
	})
	if first.workspace != st.Workspaces[1].ID || second.workspace != st.Workspaces[0].ID || st.Active != 0 {
		t.Fatal("card press changed another viewer or session selection")
	}
}
