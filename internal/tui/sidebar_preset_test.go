package tui

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/runtime"
)

func TestSidebarPresetButtonLinksRightWidth(t *testing.T) {
	for _, cols := range []int{65, 80, 100, 200} {
		for _, mode := range []string{"", "compact", "hidden"} {
			t.Run(fmt.Sprint(cols)+"/"+mode, func(t *testing.T) {
				a := NewApp(model.New(t.TempDir(), cols, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
				a.setSidebarConfig(config.Config{}, true)
				a.st.RightSidebarMode = mode
				var saved endpoint.ViewPreferences
				a.preferenceSave = func(p endpoint.ViewPreferences) error { saved = p; return nil }
				lo, hi, normal := a.st.SidebarBounds()
				for _, want := range []int{hi, lo, normal} {
					button := render.SidebarWidthRect(a.st)
					if button.W == 0 {
						t.Fatal("width button unavailable")
					}
					sidebarEvent(a, input.KindMouseClick, button.X+1, button.Y)
					sidebarEvent(a, input.KindMouseRelease, button.X+1, button.Y)
					if a.st.RightSidebarSize != want || a.st.ExpandedSidebarWidth() != want || a.st.RightSidebarMode != mode {
						t.Fatalf("want=%d left=%d right=%d mode=%q", want, a.st.SidebarSize, a.st.RightSidebarSize, a.st.RightSidebarMode)
					}
					if saved.RightSidebarWidth != want || saved.RightSidebarMode != mode {
						t.Fatal("preferences did not save right width/mode")
					}
					restored := NewApp(model.New(t.TempDir(), cols, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
					restored.setSidebarConfig(config.Config{}, true)
					restored.applyViewPreferences(&saved)
					restored.ApplyReloadedConfig(config.Config{})
					if restored.st.ExpandedRightSidebarWidth() != want || restored.st.RightSidebarMode != mode {
						t.Fatal("preferences/reload lost preset")
					}
					// Preserve narrow-terminal adaptation; the expanded preference must survive it.
					a.exec(model.CmdResize{Cols: 200, Rows: 30})
					if mode == "" && a.st.RightSidebarArea().W != want {
						t.Fatal("expanded right width not restored")
					}
					a.exec(model.CmdResize{Cols: cols, Rows: 30})
					expected := map[int]map[int]int{
						65:  {hi: 22, lo: 18, normal: 22},
						80:  {hi: 30, lo: 18, normal: 26},
						100: {hi: 40, lo: 18, normal: 26},
						200: {hi: 72, lo: 18, normal: 26},
					}[cols][want]
					if mode == "hidden" {
						expected = 0
					}
					if mode == "compact" && expected > 0 {
						expected = model.SidebarCollapsedWidth
					}
					if a.st.PanesArea().W < 20 {
						t.Fatalf("center width=%d", a.st.PanesArea().W)
					}
					if got := a.st.RightSidebarArea().W; got != expected {
						t.Fatalf("rendered right=%d want=%d", got, expected)
					}
					t.Logf("cols=%d preset=%d mode=%q rendered-right=%d", cols, want, mode, a.st.RightSidebarArea().W)
				}
			})
		}
	}
}

func TestSidebarPresetMenuUsesRightConfiguredDefault(t *testing.T) {
	for _, configured := range []int{0, 42} {
		a := NewApp(model.New(t.TempDir(), 200, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
		c := config.Config{}
		if configured > 0 {
			c.UI.RightSidebarWidth = &configured
		}
		a.setSidebarConfig(c, true)
		lo, hi, normal := a.st.SidebarBounds()
		if configured > 0 {
			normal = configured
		}
		for _, tc := range []struct {
			preset string
			width  int
		}{{"wide", hi}, {"narrow", lo}, {"normal", normal}} {
			a.openMainMenu(0, 0)
			a.activateMenu(menuIndex(a, "sidebar "+tc.preset))
			if a.st.RightSidebarSize != tc.width {
				t.Fatalf("%s configured=%d got=%d want=%d", tc.preset, configured, a.st.RightSidebarSize, tc.width)
			}
			a.ApplyReloadedConfig(c)
			if a.st.ExpandedRightSidebarWidth() != tc.width {
				t.Fatal("reload changed preset width")
			}
		}
	}
}

func TestSidebarPresetMobileMenuTap(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 60, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	a.setSidebarConfig(config.Config{}, true)
	lo, hi, normal := a.st.SidebarBounds()
	for _, tc := range []struct {
		preset string
		width  int
	}{{"wide", hi}, {"narrow", lo}, {"normal", normal}} {
		sw := render.MobileSwitchRect(a.st)
		sidebarEvent(a, input.KindMouseClick, sw.X+1, sw.Y)
		if !a.mobileSwitcher {
			t.Fatal("mobile switcher not opened")
		}
		index := menuIndexInItems(a.mainMenuItems(), "sidebar "+tc.preset)
		rowIndex := -1
		for i, row := range render.MobileRows(a.st, a.mainMenuItems()) {
			if row.Kind == render.RowMobileMenuItem && row.Line == index {
				rowIndex = i
				break
			}
		}
		if rowIndex < 0 {
			t.Fatal("mobile preset item missing")
		}
		viewport := render.MobileViewport(a.st)
		a.mobileScroll = render.MobileOffset(a.st, a.mainMenuItems(), rowIndex)
		sidebarEvent(a, input.KindMouseClick, 1, viewport.Y+rowIndex-a.mobileScroll)
		if a.mobileSwitcher || a.st.RightSidebarSize != tc.width {
			t.Fatalf("mobile tap %s: width=%d switcher=%v", tc.preset, a.st.RightSidebarSize, a.mobileSwitcher)
		}
		if a.st.SidebarArea().W != 0 || a.st.RightSidebarArea().W != 0 {
			t.Fatal("mobile visibility changed")
		}
	}
}

func menuIndexInItems(items []render.MenuItem, label string) int {
	for i, item := range items {
		if item.Label == label {
			return i
		}
	}
	return -1
}

func TestSidebarPresetDragRestoresFortyCellMinimum(t *testing.T) {
	for _, cols := range []int{65, 80, 100} {
		for _, right := range []bool{false, true} {
			a := NewApp(model.New(t.TempDir(), cols, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
			a.setSidebarConfig(config.Config{}, true)
			a.sidebarPreset("wide")
			if a.st.PanesArea().W < 20 || a.st.PanesArea().W >= model.MinimumPaneAreaWidth {
				t.Fatal("preset did not use approved narrow adaptation")
			}
			x := a.st.SidebarArea().W - 1
			if right {
				x = a.st.RightSidebarArea().X
			}
			sidebarEvent(a, input.KindMouseClick, x, 5)
			sidebarEvent(a, input.KindMouseMotion, x+1, 5)
			sidebarEvent(a, input.KindMouseRelease, x+1, 5)
			if a.st.SidebarPresetActive || a.st.PanesArea().W < model.MinimumPaneAreaWidth {
				t.Fatalf("right=%v active=%v center=%d", right, a.st.SidebarPresetActive, a.st.PanesArea().W)
			}
		}
	}
}

func TestSidebarPresetPreferencesFileAndViewerIsolation(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 80, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	a.setSidebarConfig(config.Config{}, true)
	first := a.NewViewer(&bytes.Buffer{}, 80, 30, nil)
	second := a.NewViewer(&bytes.Buffer{}, 80, 30, nil)
	path := t.TempDir() + "/preferences.json"
	first.Configure(a, nil, nil, func(p endpoint.ViewPreferences) error { return endpoint.SaveViewPreferences(path, p) })
	first.with(a, func(ui *App) { ui.sidebarPreset("wide") })
	p, err := endpoint.LoadViewPreferences(path)
	if err != nil || p == nil || !p.SidebarPresetActive || p.RightSidebarWidth != model.SidebarMaxDefault {
		t.Fatalf("preferences=%+v err=%v", p, err)
	}
	first.with(a, func(ui *App) {
		if !ui.st.SidebarPresetActive || ui.st.RightSidebarArea().W != 30 {
			t.Fatal("preset viewer lost adaptation")
		}
	})
	second.with(a, func(ui *App) {
		if ui.st.SidebarPresetActive {
			t.Fatal("preset leaked to another viewer")
		}
	})
	restored := NewApp(model.New(t.TempDir(), 80, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	restored.setSidebarConfig(config.Config{}, true)
	restored.applyViewPreferences(p)
	if restored.st.RightSidebarArea().W != 30 || restored.st.PanesArea().W < 20 {
		t.Fatal("file restore lost adaptation")
	}
	restored.st.RightSidebarMode = "hidden"
	restored.sidebarPreset("narrow")
	if restored.st.RightSidebarMode != "hidden" {
		t.Fatal("preset opened hidden sidebar")
	}
	restored.exec(model.CmdToggleRightSidebar{})
	if restored.st.RightSidebarArea().W != model.SidebarMinDefault {
		t.Fatal("reopening did not use chosen preset")
	}
}

func TestSidebarPresetDragPreservesCollapsedOtherWidth(t *testing.T) {
	for _, dragRight := range []bool{false, true} {
		t.Run(fmt.Sprint(dragRight), func(t *testing.T) {
			a := NewApp(model.New(t.TempDir(), 200, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
			a.setSidebarConfig(config.Config{}, true)
			path := t.TempDir() + "/preferences.json"
			a.preferenceSave = func(p endpoint.ViewPreferences) error { return endpoint.SaveViewPreferences(path, p) }
			a.sidebarPreset("wide")
			if dragRight {
				a.exec(model.CmdToggleSidebar{})
			} else {
				a.exec(model.CmdToggleRightSidebar{})
			}
			x := a.st.SidebarArea().W - 1
			if dragRight {
				x = a.st.RightSidebarArea().X
			}
			sidebarEvent(a, input.KindMouseClick, x, 5)
			sidebarEvent(a, input.KindMouseMotion, x+1, 5)
			sidebarEvent(a, input.KindMouseRelease, x+1, 5)
			saved, err := endpoint.LoadViewPreferences(path)
			if err != nil || saved == nil {
				t.Fatal(saved, err)
			}
			retained, persisted := a.st.RightSidebarSize, saved.RightSidebarWidth
			if dragRight {
				retained, persisted = a.st.SidebarSize, saved.SidebarWidth
			}
			if retained != model.SidebarMaxDefault || persisted != model.SidebarMaxDefault {
				t.Fatalf("dragRight=%v other requested=%d saved=%d want=%d", dragRight, retained, persisted, model.SidebarMaxDefault)
			}
			if a.st.SidebarPresetActive || a.st.PanesArea().W < model.MinimumPaneAreaWidth {
				t.Fatal("drag did not restore normal center constraint")
			}
			if dragRight {
				a.exec(model.CmdToggleSidebar{})
				a.exec(model.CmdToggleSidebar{})
			} else {
				a.exec(model.CmdToggleRightSidebar{})
				a.exec(model.CmdToggleRightSidebar{})
			}
			width := a.st.RightSidebarArea().W
			if dragRight {
				width = a.st.SidebarArea().W
			}
			if width != model.SidebarMaxDefault {
				t.Fatalf("reopened width=%d", width)
			}
		})
	}
}
