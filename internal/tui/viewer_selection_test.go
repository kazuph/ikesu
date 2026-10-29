package tui

import (
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
	"io"
	"testing"
)

func TestViewerReconnectSelectionSurvivingIDsAndFallback(t *testing.T) {
	st := model.New(t.TempDir(), 120, 40)
	st.Apply(model.CmdNewWorkspace{})
	st.Apply(model.CmdNewTab{Workspace: 1})
	st.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	saved := endpoint.CaptureSelection(st)
	app := NewApp(st, runtime.NewSupervisor(), io.Discard, "")
	for _, scenario := range []string{"survives", "pane gone", "tab gone", "workspace gone"} {
		t.Run(scenario, func(t *testing.T) {
			switch scenario {
			case "pane gone":
				st.Apply(model.CmdClosePane{Pane: saved.Pane})
			case "tab gone":
				st.Apply(model.CmdCloseTab{Workspace: 1, Tab: 1})
			case "workspace gone":
				st.Apply(model.CmdCloseWorkspace{Index: 1})
			}
			viewer := app.NewViewer(io.Discard, 120, 40, nil)
			viewer.RestoreSelection(app, saved)
			viewer.with(app, func(ui *App) {
				got := endpoint.CaptureSelection(ui.st)
				wantWs, wantTab, wantPane := 2, 3, saved.Pane
				switch scenario {
				case "pane gone":
					wantPane = 3
				case "tab gone":
					wantTab, wantPane = 2, 2
				case "workspace gone":
					wantWs, wantTab, wantPane = 1, 1, 1
				}
				if got.Workspace != wantWs || got.Tabs[wantWs] != wantTab || got.Pane != wantPane {
					t.Fatalf("selection=%+v want %d/%d/%d", got, wantWs, wantTab, wantPane)
				}
			})
		})
	}
}
