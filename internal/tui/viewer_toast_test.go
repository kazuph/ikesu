package tui

import (
	"bytes"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestEndpointToastVisibilityFollowsDeliveryPositionAndViewerTick(t *testing.T) {
	teststate.Lock(t)
	state := model.New(t.TempDir(), 160, 45)
	a := NewApp(state, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	a.SetServerUI()
	cfg, err := config.Decode([]byte("[ui.toast]\ndelivery='herdr'\ndelay_seconds=0\n[ui.toast.herdr]\nposition='top-right'\n"))
	if err != nil {
		t.Fatal(err)
	}
	a.SetSidebarConfig(cfg, false)
	first := a.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	second := a.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	first.ShowToast(a, "finished", "context", "finished", state.FocusedPane())
	second.ShowToast(a, "finished", "context", "finished", state.FocusedPane())
	if first.app.toast.shown || second.app.toast.shown {
		t.Fatal("server toast bypassed its scheduled transition")
	}
	first.Tick(a)
	second.Tick(a)
	first.with(a, func(ui *App) {
		if !ui.toastVisible() {
			t.Fatal("viewer tick did not publish its scheduled toast")
		}
	})
	if a.toast != nil || first.app.toast == second.app.toast {
		t.Fatal("viewer toasts share mutable dismissal state")
	}
	first.with(a, func(ui *App) { ui.DismissToast() })
	if second.app.toast == nil {
		t.Fatal("one viewer dismissed another viewer's toast")
	}
	for _, delivery := range []string{"off", "terminal", "system"} {
		second.with(a, func(ui *App) {
			ui.sidebarConfig.UI.Toast.Delivery = delivery
			if ui.toastVisible() {
				t.Fatal("endpoint showed a notification for another delivery route", delivery)
			}
		})
	}
	second.with(a, func(ui *App) {
		ui.sidebarConfig.UI.Toast.Delivery = "herdr"
		ui.sidebarConfig.UI.Toast.Herdr.Position = ""
		if ui.toastVisible() {
			t.Fatal("desktop displayed a position-less notification")
		}
		toast := ui.toast
		ui.openToastTarget()
		if ui.toast != toast {
			t.Fatal("hidden notification target was opened/dismissed")
		}
		ui.sidebarConfig.UI.Toast.Herdr.Position = "bottom-left"
		if !ui.toastVisible() {
			t.Fatal("positioned notification stayed hidden")
		}
	})
}
