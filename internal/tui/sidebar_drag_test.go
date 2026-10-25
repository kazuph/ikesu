package tui

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
)

func TestSidebarDragIndependentContinuousAndRestored(t *testing.T) {
	for _, right := range []bool{false, true} {
		name := "left"
		if right {
			name = "right"
		}
		t.Run(name, func(t *testing.T) {
			a := NewApp(model.New(t.TempDir(), 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
			a.setSidebarConfig(config.Config{}, true)
			var saved endpoint.ViewPreferences
			a.preferenceSave = func(p endpoint.ViewPreferences) error { saved = p; return nil }
			start := a.st.SidebarArea().W - 1
			if right {
				start = a.st.RightSidebarArea().X
			}
			sidebarEvent(a, input.KindMouseClick, start, 5)
			for _, width := range []int{35, 18, 17, 10, 5, 3, 2, 3, 17, 18, 35, 72, 100, 2} {
				x := width - 1
				if right {
					x = a.st.Cols - width
				}
				sidebarEvent(a, input.KindMouseMotion, x, 5)
				want := min(72, width)
				l, r := a.st.SidebarArea().W, a.st.RightSidebarArea().W
				if right {
					if r != want || l != 26 {
						t.Fatalf("width %d: left=%d right=%d", width, l, r)
					}
				} else if l != want || r != 26 {
					t.Fatalf("width %d: left=%d right=%d", width, l, r)
				}
				if a.st.PanesArea().W < model.MinimumPaneAreaWidth {
					t.Fatal("pane minimum violated")
				}
				if right {
					if (a.st.RightSidebarMode == "compact") != (width < 18) {
						t.Fatal("right mode")
					}
				} else if a.st.SidebarVisible != (width >= 18) {
					t.Fatal("left mode")
				}
			}
			released := a.st.SidebarArea().W - 1
			if right {
				released = a.st.Cols - a.st.RightSidebarArea().W
			}
			sidebarEvent(a, input.KindMouseRelease, released, 5)
			encoded, _ := json.Marshal(saved)
			var fields map[string]any
			_ = json.Unmarshal(encoded, &fields)
			if right && fields["right_sidebar_width"] != float64(2) {
				t.Fatalf("right width not saved: %s", encoded)
			}
			if !right && fields["sidebar_width"] != float64(2) {
				t.Fatalf("left width not saved: %s", encoded)
			}
			restored := NewApp(model.New(t.TempDir(), 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
			restored.setSidebarConfig(config.Config{}, true)
			restored.applyViewPreferences(&saved)
			if restored.st.SidebarArea() != a.st.SidebarArea() || restored.st.RightSidebarArea() != a.st.RightSidebarArea() {
				t.Fatalf("restored %v/%v, want %v/%v", restored.st.SidebarArea(), restored.st.RightSidebarArea(), a.st.SidebarArea(), a.st.RightSidebarArea())
			}
		})
	}
}

func TestSidebarDragRespectsPaneMinimum(t *testing.T) {
	for _, right := range []bool{false, true} {
		a := NewApp(model.New(t.TempDir(), 100, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
		start := 25
		x := 90
		if right {
			start = 74
			x = 0
		}
		sidebarEvent(a, input.KindMouseClick, start, 5)
		sidebarEvent(a, input.KindMouseMotion, x, 5)
		if a.st.PanesArea().W != model.MinimumPaneAreaWidth || a.st.SidebarArea().W+a.st.RightSidebarArea().W != 60 {
			t.Fatalf("right=%t: areas %v %v %v", right, a.st.SidebarArea(), a.st.RightSidebarArea(), a.st.PanesArea())
		}
	}
}

func TestRightSidebarDefaultIndependentOfLiveLeft(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	width := 31
	c := config.Config{}
	c.UI.SidebarWidth = &width
	a.setSidebarConfig(c, true)
	a.st.SidebarSize = 45
	if got := a.st.RightSidebarArea().W; got != 31 {
		t.Fatalf("right default follows current left: %d", got)
	}
}

func TestNarrowSidebarActualPTYClickTargetsAndToggle(t *testing.T) {
	for _, width := range []int{2, 3, 5, 10, 17} {
		a := specRuntimeApp(t)
		a.st.Cols, a.st.Rows = 160, 30
		if err := a.StartInitialPane(); err != nil {
			t.Fatal(err)
		}
		a.exec(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
		a.st.SidebarSize, a.st.RightSidebarSize = width, width
		a.st.SidebarVisible = false
		a.st.RightSidebarMode = "compact"
		a.SetReported(1, "codex", "idle")
		a.SetReported(2, "codex", "idle")
		sidebarEvent(a, input.KindMouseClick, width-2, 7)
		sidebarEvent(a, input.KindMouseRelease, width-2, 7)
		if a.st.Active != 1 {
			t.Fatalf("width=%d workspace name click missed", width)
		}
		sidebarEvent(a, input.KindMouseClick, a.st.RightSidebarArea().X+width-1, 2)
		if a.st.Active != 0 || a.focusedPane() != 1 {
			t.Fatalf("width=%d right name click missed", width)
		}
		a.st.ToggleSidebar()
		a.st.ToggleSidebar()
		a.st.ToggleSidebar()
		a.st.ToggleRightSidebar()
		a.st.ToggleRightSidebar()
		a.st.ToggleRightSidebar()
		if a.st.SidebarArea().W != 2 || a.st.RightSidebarArea().W != 2 {
			t.Fatal("click/key cycle must return to one-column mode")
		}
	}
}

func TestSidebarDragNeverHidesWithCollapsedHiddenSetting(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	a.st.SidebarCollapsedHidden = true
	sidebarEvent(a, input.KindMouseClick, 25, 5)
	for _, width := range []int{10, 2, 0} {
		sidebarEvent(a, input.KindMouseMotion, width-1, 5)
		if a.st.SidebarArea().W != max(2, width) {
			t.Fatalf("drag width %d hidden by setting: %v", width, a.st.SidebarArea())
		}
	}
}

func TestSidebarRightManualWidthSurvivesReload(t *testing.T) {
	for _, server := range []bool{false, true} {
		a := NewApp(model.New(t.TempDir(), 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
		if server {
			a.SetServerUI()
		}
		c := config.Config{}
		configured := 42
		c.UI.RightSidebarWidth = &configured
		a.setSidebarConfig(c, true)
		sidebarEvent(a, input.KindMouseClick, 25, 5)
		sidebarEvent(a, input.KindMouseMotion, 34, 5)
		sidebarEvent(a, input.KindMouseRelease, 34, 5)
		sidebarEvent(a, input.KindMouseClick, a.st.RightSidebarArea().X, 5)
		sidebarEvent(a, input.KindMouseMotion, 150, 5)
		sidebarEvent(a, input.KindMouseRelease, 150, 5)
		a.ApplyReloadedConfig(c)
		if a.st.RightSidebarArea().W != 10 || a.st.RightSidebarMode != "compact" {
			t.Fatalf("server=%t reload lost right manual width: %v mode=%s", server, a.st.RightSidebarArea(), a.st.RightSidebarMode)
		}
	}
}

func TestSidebarRightManualReloadThemeToggleRestoreAndDefault(t *testing.T) {
	for _, server := range []bool{false, true} {
		a := NewApp(model.New(t.TempDir(), 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
		if server {
			a.SetServerUI()
		}
		c := config.Config{}
		configured := 42
		c.UI.RightSidebarWidth = &configured
		a.setSidebarConfig(c, true)
		var saved endpoint.ViewPreferences
		a.preferenceSave = func(p endpoint.ViewPreferences) error { saved = p; return nil }
		for _, width := range []int{10, 35} {
			sidebarEvent(a, input.KindMouseClick, a.st.SidebarArea().W-1, 5)
			sidebarEvent(a, input.KindMouseMotion, 34, 5)
			sidebarEvent(a, input.KindMouseRelease, 34, 5)
			x := a.st.Cols - width
			sidebarEvent(a, input.KindMouseClick, a.st.RightSidebarArea().X, 5)
			sidebarEvent(a, input.KindMouseMotion, x, 5)
			sidebarEvent(a, input.KindMouseRelease, x, 5)
			check := func(stage string) {
				t.Helper()
				if a.st.RightSidebarArea().W != width || (a.st.RightSidebarMode == "compact") != (width < 18) {
					t.Fatalf("%s server=%t width=%d actual=%v mode=%s", stage, server, width, a.st.RightSidebarArea(), a.st.RightSidebarMode)
				}
			}
			a.ApplyReloadedConfig(c)
			check("reload")
			c.Theme.Name = "catppuccin-latte"
			a.ApplyReloadedConfig(c)
			check("theme")
			a.exec(model.CmdToggleSidebar{})
			check("left toggle")
			a.saveViewPreferences()
			restored := NewApp(model.New(t.TempDir(), 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
			restored.setSidebarConfig(c, true)
			restored.applyViewPreferences(&saved)
			restored.ApplyReloadedConfig(c)
			if restored.st.RightSidebarArea().W != width || !restored.st.RightSidebarManual {
				t.Fatal("manual metadata not restored")
			}
		}
		g := &sidebarGesture{right: true, double: true, divider: true}
		a.finishSidebarDivider(g, time.Now())
		if a.st.RightSidebarArea().W != 42 || a.st.RightSidebarManual {
			t.Fatal("double click must reset manual and restore configured width")
		}
		configured = 38
		a.ApplyReloadedConfig(c)
		if a.st.RightSidebarArea().W != 38 {
			t.Fatal("default view must follow config after double click")
		}
	}
}
