package tui

import (
	"bytes"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestViewerReloadKeepsManualWidthCollapsedStateAndSessionDisplay(t *testing.T) {
	st := model.New(t.TempDir(), 160, 45)
	a := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	v := a.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	c, err := config.Decode([]byte("onboarding=false\n[ui]\nsidebar_width=26\n"))
	if err != nil {
		t.Fatal(err)
	}
	v.Configure(a, &c, &endpoint.ViewPreferences{SidebarWidth: 42, SidebarManual: true, SidebarCollapsed: true}, nil)
	v.SetConfigEditor(func(config.Edit, func(config.Config, error)) {})
	initial := captureDisplay(st)
	reloaded, err := config.Decode([]byte("onboarding=false\n[ui]\nsidebar_width=38\nsidebar_start_collapsed=true\nshow_tab_bar=false\npane_borders=false\n"))
	if err != nil {
		t.Fatal(err)
	}
	v.ConfigurationResult(a, func(c config.Config, err error) {
		if err != nil {
			t.Fatal(err)
		}
		v.ApplyReloadedConfig(c)
	}, reloaded, nil)
	if v.display.size != 42 || !v.display.manual || v.display.visible || v.display.tabs || v.display.borders {
		t.Fatal(v.display)
	}
	if captureDisplay(st) != initial {
		t.Fatal("viewer reload overwrote session display", captureDisplay(st), initial)
	}
	second := a.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	second.Configure(a, &c, nil, nil)
	second.SetConfigEditor(func(config.Edit, func(config.Config, error)) {})
	second.ConfigurationResult(a, func(c config.Config, err error) { second.ApplyReloadedConfig(c) }, reloaded, nil)
	if second.display.size != 38 || !second.display.visible || second.display.manual {
		t.Fatal(second.display)
	}
	if v.display.size != 42 || v.display.visible {
		t.Fatal("another reload changed viewer preference")
	}
}
