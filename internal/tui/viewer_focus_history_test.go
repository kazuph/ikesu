package tui

import (
	"bytes"
	"slices"
	"testing"

	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func TestNewSavedMachineViewerCanFocusAdjacentPaneAfterSessionHistory(t *testing.T) {
	app := specRuntimeApp(t)
	first := app.focusedPane()
	app.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	second := app.focusedPane()
	app.exec(model.CmdFocusPane{Pane: first})
	if app.focusHistoryAt < 1 {
		t.Fatal("session did not build real focus history")
	}
	savedHistory := append([]int(nil), app.focusHistory...)
	savedPosition := app.focusHistoryAt
	viewer := app.NewViewer(&bytes.Buffer{}, 120, 40, nil)
	viewer.with(app, func(ui *App) {
		ui.exec(model.CmdFocusPane{Pane: second})
		if ui.focusedPane() != second {
			t.Fatal("new viewer did not focus the adjacent pane")
		}
		ui.focusHistoryStep(false)
		if ui.focusedPane() != first {
			t.Fatal("new viewer did not preserve its first focus transition")
		}
	})
	if app.focusHistoryAt != savedPosition || !slices.Equal(app.focusHistory, savedHistory) {
		t.Fatal("viewer focus mutated the session history")
	}
}
