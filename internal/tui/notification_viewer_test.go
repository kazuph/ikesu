package tui

import (
	"bytes"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestStage6aSessionToastReachesViewerAndStaysDismissed(t *testing.T) {
	app := NewApp(model.New(t.TempDir(), 80, 24), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	viewer := app.NewViewer(&bytes.Buffer{}, 80, 24, nil)
	app.ShowToast("日本語", "本文", "info", -1)
	viewer.with(app, func(ui *App) {
		if ui.toast == nil || ui.toast.title != "日本語" || ui.toast.body != "本文" {
			t.Fatal("session toast did not reach connected viewer")
		}
		ui.DismissToast()
	})
	viewer.with(app, func(ui *App) {
		if ui.toast != nil {
			t.Fatal("dismissed toast reappeared on redraw")
		}
	})
	app.ShowToast("次の通知", "", "info", -1)
	viewer.with(app, func(ui *App) {
		if ui.toast == nil || ui.toast.title != "次の通知" {
			t.Fatal("next notification was lost")
		}
		ui.toast.until = time.Now().Add(-time.Second)
	})
	viewer.Tick(app)
	viewer.with(app, func(ui *App) {
		if ui.toast != nil {
			t.Fatal("expired toast remains in connected viewer")
		}
	})
}
