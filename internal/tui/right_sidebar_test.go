package tui

import (
	"bytes"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
	"testing"
)

func TestRightSidebarViewerPreferencesIndependentAndRestored(t *testing.T) {
	st := model.New(t.TempDir(), 160, 45)
	app := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	first := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	second := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	var saved endpoint.ViewPreferences
	first.Configure(app, &config.Config{}, nil, func(p endpoint.ViewPreferences) error { saved = p; return nil })
	first.with(app, func(ui *App) {
		ui.exec(model.CmdToggleSidebar{})
		ui.exec(model.CmdToggleSidebar{})
		ui.exec(model.CmdToggleRightSidebar{})
	})
	if !saved.SidebarHidden || saved.RightSidebarMode != "compact" || !saved.SidebarCollapsed {
		t.Fatalf("saved=%+v", saved)
	}
	second.with(app, func(ui *App) {
		if !ui.st.SidebarVisible || ui.st.SidebarHidden || ui.st.RightSidebarMode != "" {
			t.Fatal("another viewer changed")
		}
	})
	restored := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	restored.Configure(app, &config.Config{}, &saved, nil)
	restored.with(app, func(ui *App) {
		if ui.st.SidebarArea().W != 0 || ui.st.RightSidebarArea().W != 2 {
			t.Fatal("not restored")
		}
	})
}
