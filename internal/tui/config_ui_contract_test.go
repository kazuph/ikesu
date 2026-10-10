package tui

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestConfigUIChangesActualStateAndRenderedCells(t *testing.T) {
	teststate.Lock(t)
	for _, tc := range []struct{ key, first, second string }{
		{"sidebar_width", "sidebar_width=26", "sidebar_width=40"},
		{"sidebar_min_width", "sidebar_width=20\nsidebar_min_width=18", "sidebar_width=20\nsidebar_min_width=30"},
		{"sidebar_max_width", "sidebar_width=70\nsidebar_max_width=40", "sidebar_width=70\nsidebar_max_width=60"},
		{"sidebar_start_collapsed", "sidebar_start_collapsed=false", "sidebar_start_collapsed=true"},
		{"sidebar_collapsed_mode", "sidebar_start_collapsed=true\nsidebar_collapsed_mode='compact'", "sidebar_start_collapsed=true\nsidebar_collapsed_mode='hidden'"},
		{"mobile_width_threshold", "mobile_width_threshold=64", "mobile_width_threshold=130"},
		{"pane_borders", "pane_borders=true", "pane_borders=false"},
		{"show_tab_bar", "show_tab_bar=true", "show_tab_bar=false"},
		{"hide_tab_bar_when_single_tab", "hide_tab_bar_when_single_tab=false", "hide_tab_bar_when_single_tab=true"},
		{"transparent_sidebar_cards", "transparent_sidebar_cards=false", "transparent_sidebar_cards=true"},
		{"agent_panel_sort", "agent_panel_sort='spaces'", "agent_panel_sort='priority'"},
		{"agent_panel_scope", "agent_panel_scope='all'", "agent_panel_scope='sort'"},
		{"workspace_panel_density", "workspace_panel_density='full'", "workspace_panel_density='slim'"},
		{"accent", "accent='#123456'", "accent='#654321'"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			a := specRuntimeApp(t)
			a.SetServerUI()
			for i := 0; i < 3; i++ {
				if i > 0 {
					a.exec(model.CmdNewWorkspace{Name: fmt.Sprintf("workspace%d", i), Cwd: a.cwd})
				}
				pid := a.focusedPane()
				if a.sup.Get(pid) == nil {
					if _, err := a.sup.SpawnWith(pid, a.cwd, 80, 20, nil, []string{"/bin/cat"}); err != nil {
						t.Fatal(err)
					}
				}
				a.SetReported(pid, "codex", []string{"idle", "working", "blocked"}[i])
			}
			a.exec(model.CmdSelectWorkspace{Index: 0})
			a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
			a.SetReported(a.focusedPane(), "claude", "idle")
			var frames []*render.Frame
			for _, source := range []string{tc.first, tc.second} {
				c, err := config.Decode([]byte("[ui]\n" + source + "\n"))
				if err != nil || len(c.Warnings) > 0 {
					t.Fatal(c.Warnings, err)
				}
				a.st.SidebarVisible = true
				a.st.SidebarManual = false
				a.SetSidebarConfig(c, true)
				a.SetUIFlags(c.ShowTabBar(), c.HideTabBarWhenSingleTab(), c.PaneBorders())
				frames = append(frames, render.Compose(render.Input{State: a.st, ServerUI: true, SidebarConfig: c, Panes: a.paneSnaps()}).Frame)
			}
			if reflect.DeepEqual(frames[0].Cells, frames[1].Cells) {
				t.Fatal("setting did not change any rendered cell")
			}
		})
	}
}

func TestConfigHostMouseCaptureAndFocusRedraw(t *testing.T) {
	teststate.Lock(t)
	for _, on := range []bool{false, true} {
		a := specRuntimeApp(t)
		var out bytes.Buffer
		a.out = &out
		c, err := config.Decode([]byte(fmt.Sprintf("[ui]\nmouse_capture=%t\nredraw_on_focus_gained=%t\n", on, on)))
		if err != nil {
			t.Fatal(err)
		}
		a.SetSidebarConfig(c, false)
		out.Reset()
		a.BeginHostMouse(1200, 700)
		if bytes.Contains(out.Bytes(), []byte("\x1b[?1016$p")) != on {
			t.Fatal("capture query", on, out.String())
		}
		a.dirty = false
		specFeed(a, "\x1b[I")
		if a.dirty != on {
			t.Fatal("redraw state", on, a.dirty)
		}
	}
}

func TestConfigCustomDarkAndLightNamesReachPalette(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	c, err := config.Decode([]byte("[theme]\nname='catppuccin'\nauto_switch=true\ndark_name='dracula'\nlight_name='one-light'\n"))
	if err != nil {
		t.Fatal(err)
	}
	a.SetSidebarConfig(c, false)
	for _, tc := range []struct{ input, name string }{{"\x1b[?997;1n", "dracula"}, {"\x1b[?997;2n", "one-light"}} {
		specFeed(a, tc.input)
		if a.sidebarConfig.ThemeName() != tc.name || a.panePalette.Accent != render.ThemePalette(tc.name).Accent {
			t.Fatal(tc.name, a.sidebarConfig.ThemeName(), a.panePalette)
		}
	}
}

func TestConfigConfirmCloseAndNewTabPromptChangeInputBehavior(t *testing.T) {
	teststate.Lock(t)
	for _, enabled := range []bool{false, true} {
		a := specRuntimeApp(t)
		c, err := config.Decode([]byte(fmt.Sprintf("[ui]\nconfirm_close=%t\nprompt_new_tab_name=%t\n", enabled, enabled)))
		if err != nil {
			t.Fatal(err)
		}
		a.SetSidebarConfig(c, false)
		a.exec(model.CmdNewWorkspace{Name: "second", Cwd: a.cwd})
		specFeed(a, "\x02D")
		if (a.dialog != nil) != enabled || (len(a.st.Workspaces) == 1) == enabled {
			t.Fatal("close confirmation", enabled, a.dialog, len(a.st.Workspaces))
		}
		a.dialog = nil
		before := len(a.st.ActiveWorkspace().Tabs)
		specFeed(a, "\x02c")
		if (a.dialog != nil) != enabled || (len(a.st.ActiveWorkspace().Tabs) == before+1) == enabled {
			t.Fatal("new tab prompt", enabled)
		}
	}
}

func TestConfigToastDelayChangesActualScheduledState(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.SetServerUI()
	for _, seconds := range []int{0, 3} {
		c, err := config.Decode([]byte(fmt.Sprintf("[ui.toast]\ndelay_seconds=%d\n", seconds)))
		if err != nil {
			t.Fatal(err)
		}
		a.SetSidebarConfig(c, false)
		a.ShowToast("notice", "body", "finished", a.focusedPane())
		a.expireToast()
		if a.toast.shown != (seconds == 0) {
			t.Fatal(seconds, a.toast)
		}
	}
}

func TestConfigClipboardFeedbackEnabledChangesVisibleState(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	for _, enabled := range []bool{false, true} {
		c, err := config.Decode([]byte(fmt.Sprintf("[ui.toast.clipboard]\nenabled=%t\n", enabled)))
		if err != nil {
			t.Fatal(err)
		}
		a.SetSidebarConfig(c, false)
		a.copied = "Copied 3 lines"
		a.copiedAt = time.Now()
		if (a.copiedFlash() != "") != enabled {
			t.Fatal(enabled, a.copiedFlash())
		}
	}
}
