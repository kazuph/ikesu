package tui

import (
	"bytes"
	"testing"

	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestViewerSidebarBothColumnsPersistWithoutChangingOtherView(t *testing.T) {
	{
		st := model.New(t.TempDir(), 160, 45)
		app := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
		first := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
		second := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
		initial := captureDisplay(st)
		var saved []endpoint.ViewPreferences
		first.Configure(app, nil, nil, func(p endpoint.ViewPreferences) error { saved = append(saved, p); return nil })
		first.with(app, func(ui *App) {
			width := ui.st.SidebarArea().W
			x := width - 1
			// The divider column remains draggable; adjacent pane cells belong to the pane.
			wantWidth := x + 8 + 1
			sidebarEvent(ui, input.KindMouseClick, x, 10)
			sidebarEvent(ui, input.KindMouseMotion, x+8, 10)
			if len(saved) != 0 {
				t.Fatal("drag saved before release")
			}
			sidebarEvent(ui, input.KindMouseRelease, x+8, 10)
			if len(saved) != 1 || saved[0].SidebarWidth != wantWidth || !saved[0].SidebarManual {
				t.Fatalf("saved=%+v", saved)
			}
			ui.exec(model.CmdToggleRightSidebar{})
			if len(saved) != 2 || saved[1].RightSidebarMode != "compact" || saved[1].SidebarSectionSplit != nil {
				t.Fatalf("right state not saved: %+v", saved)
			}
			ui.exec(model.CmdToggleSidebar{})
			if len(saved) != 3 || !saved[2].SidebarCollapsed {
				t.Fatal("collapse not saved")
			}
		})
		second.with(app, func(ui *App) {
			if ui.st.SidebarSize != initial.size || ui.st.SidebarVisible != initial.visible || ui.st.SidebarSectionSplit != initial.split {
				t.Fatal("other viewer changed")
			}
		})
		if st.SidebarSize != initial.size || st.SidebarVisible != initial.visible || st.SidebarSectionSplit != initial.split {
			t.Fatal("session preferences changed")
		}
	}
}
