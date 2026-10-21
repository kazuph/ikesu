package tui

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
)

func TestStage6aExplicitNotificationKeysPreserveToastAndNormalTargets(t *testing.T) {
	app := NewApp(model.New(t.TempDir(), 80, 24), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	app.SetNavKey('o')
	app.ShowNotification("日本語", "", "")
	app.handleKey(uv.Key{Code: uv.KeyEscape}, []byte{27})
	app.prefixKey(uv.Key{Code: 'o'})
	if !app.NotificationBusy() {
		t.Fatal("explicit notification dismissed by Escape or prefix+o")
	}
	app.configAction("open_notification_target", uv.Key{})
	if !app.NotificationBusy() {
		t.Fatal("configured notification key dismissed targetless notification")
	}
	app.DismissToast()
	app.ShowToast("通常通知", "本文", "info", app.st.FocusedPane())
	app.prefixKey(uv.Key{Code: 'o'})
	if app.toast != nil {
		t.Fatal("normal targeted notification no longer dismisses after navigation")
	}
}

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

func TestStage6aExplicitToastWithoutTargetIgnoresClick(t *testing.T) {
	app := NewApp(model.New(t.TempDir(), 80, 24), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	if !app.ShowNotification("日本語", "", "") {
		t.Fatal("initial notification rejected")
	}
	app.toastRect = layout.Rect{X: 68, Y: 0, W: 12, H: 3}
	if app.toastClick(73, 2) || app.toast == nil {
		t.Fatal("targetless notification was dismissed by a click")
	}
	if app.ShowNotification("次の通知", "", "") {
		t.Fatal("busy notification replaced")
	}
	app.DismissToast()
	app.ShowToast("agent restore", "summary", "info", -1)
	if !app.toastClick(73, 2) || app.toast != nil {
		t.Fatal("existing local toast can no longer be dismissed")
	}
}

func TestStage6aExplicitNotificationUsesObservedThreeSecondLifetime(t *testing.T) {
	app := NewApp(model.New(t.TempDir(), 80, 24), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	before := time.Now()
	app.ShowNotification("日本語", "", "")
	after := time.Now()
	if app.toast.until.Before(before.Add(3*time.Second)) || app.toast.until.After(after.Add(3*time.Second)) {
		t.Fatalf("explicit API notification lifetime differs from observed 3s: %s", app.toast.until.Sub(before))
	}
}
