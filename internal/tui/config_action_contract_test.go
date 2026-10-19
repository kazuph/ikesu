package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

// Every supported action travels through the host byte decoder, rather than
// calling the action dispatcher. The old chord is tried before the new one.
func TestConfigEveryActionRemapThroughRealInput(t *testing.T) {
	teststate.Lock(t)
	names := make([]string, 0, len(config.ActionDefaults))
	for name := range config.ActionDefaults {
		if name != "remote_image_paste" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			a, _ := worktreeApp(t)
			teststate.Setenv(t, "EDITOR", "/bin/cat")
			teststate.Setenv(t, config.EnvVar, filepath.Join(t.TempDir(), "config.toml"))
			p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", "printf READY; exec /bin/cat"})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "READY")
			t.Cleanup(a.removeEditorFiles)
			binding := "cmd+y"
			if name == "switch_tab" || name == "switch_workspace" || name == "focus_agent" {
				binding = "cmd+shift+1..9"
			}
			cfg, err := config.Decode([]byte(fmt.Sprintf("[keys]\n%s=%q\n", name, binding)))
			if err != nil || len(cfg.Warnings) != 0 {
				t.Fatal(cfg.Warnings, err)
			}
			a.SetSidebarConfig(cfg, false)
			a.SetPaneKeybinds(cfg)
			check := prepareRemapAction(t, a, name)
			if check() {
				t.Fatal("precondition already satisfies action")
			}
			if old := config.ActionDefaults[name]; old != "" {
				feedConfigChord(t, a, old)
				if check() {
					t.Fatal("old binding still performs action", old)
				}
			}
			feedConfigChord(t, a, binding)
			if !check() {
				t.Fatal("new binding did not perform action", binding)
			}
		})
	}
}

func feedConfigChord(t *testing.T, a *App, spec string) {
	t.Helper()
	c, err := config.ParseChord(spec)
	if err != nil {
		t.Fatal(err)
	}
	if c.Prefix {
		specFeed(a, "\x02")
	}
	k := c.Key
	if c.Indexed {
		k.Code = '2'
	}
	raw := ""
	switch k.Code {
	case uv.KeyUp:
		raw = "\x1b[A"
	case uv.KeyDown:
		raw = "\x1b[B"
	case uv.KeyLeft:
		raw = "\x1b[D"
	case uv.KeyRight:
		raw = "\x1b[C"
	case uv.KeyTab:
		raw = "\t"
		if k.Mod.Contains(uv.ModShift) {
			raw = "\x1b[Z"
		}
	default:
		mods := 1
		if k.Mod.Contains(uv.ModShift) {
			mods += 1
		}
		if k.Mod.Contains(uv.ModAlt) {
			mods += 2
		}
		if k.Mod.Contains(uv.ModCtrl) {
			mods += 4
		}
		if k.Mod.Contains(uv.ModSuper) {
			mods += 8
		}
		raw = fmt.Sprintf("\x1b[%d;%du", k.Code, mods)
	}
	specFeed(a, raw)
}

func prepareRemapAction(t *testing.T, a *App, name string) func() bool {
	t.Helper()
	first := a.focusedPane()
	tab := func() *model.Tab { return a.st.ActiveWorkspace().TabRef() }
	count := func() int { return tab().Layout.PaneCount() }
	dialog := func(s string) func() bool {
		return func() bool { return a.dialog != nil && strings.Contains(a.dialog.d.Title, s) }
	}
	twoPanes := func(dir layout.Direction) int { a.exec(model.CmdSplitFocused{Dir: dir}); return a.focusedPane() }
	switch name {
	case "help":
		return func() bool { return a.help != nil }
	case "settings":
		return func() bool { return a.settings != nil }
	case "detach":
		a.SetDetacher(func(string) { a.exec(model.CmdQuit{}) })
		return func() bool { return a.quit }
	case "reload_config":
		if err := os.WriteFile(config.Path(), []byte("[theme]\nname='dracula'\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return func() bool { return a.sidebarConfig.ThemeName() == "dracula" }
	case "workspace_picker":
		return func() bool { return a.workspaceNavigate }
	case "goto":
		return func() bool { return a.navigator != nil }
	case "new_workspace":
		return func() bool { return len(a.st.Workspaces) == 2 }
	case "new_worktree":
		return dialog("New worktree")
	case "open_worktree":
		return func() bool { return a.menu != nil && len(a.menu.m.Items) > 0 }
	case "remove_worktree":
		return dialog("delete worktree checkout")
	case "rename_workspace":
		return dialog("Rename workspace")
	case "close_workspace":
		return dialog("Close workspace")
	case "rename_tab":
		return dialog("Rename tab")
	case "rename_pane":
		return dialog("Rename pane")
	case "new_tab":
		return dialog("New tab")
	case "previous_workspace", "next_workspace", "switch_workspace":
		a.exec(model.CmdNewWorkspace{Name: "second", Cwd: a.cwd})
		a.exec(model.CmdSelectWorkspace{Index: 0})
		return func() bool { return a.st.Active == 1 }
	case "previous_tab", "next_tab", "switch_tab":
		a.exec(model.CmdNewTab{Workspace: 0})
		a.exec(model.CmdSelectTab{Workspace: 0, Tab: 0})
		return func() bool { return a.st.ActiveWorkspace().ActiveTab == 1 }
	case "close_tab":
		a.exec(model.CmdNewTab{Workspace: 0})
		return func() bool { return len(a.st.ActiveWorkspace().Tabs) == 1 || a.dialog != nil }
	case "split_vertical", "split_horizontal":
		return func() bool { return count() == 2 }
	case "close_pane":
		twoPanes(layout.Vertical)
		return func() bool { return count() == 1 }
	case "zoom", "fullscreen":
		second := twoPanes(layout.Vertical)
		return func() bool { return tab().Zoomed == second }
	case "resize_mode":
		return func() bool { return a.resizeMode }
	case "toggle_right_sidebar":
		mode := a.st.RightSidebarMode
		return func() bool { return a.st.RightSidebarMode != mode }
	case "toggle_sidebar":
		visible := a.st.SidebarVisible
		return func() bool { return a.st.SidebarVisible != visible }
	case "copy_mode":
		return func() bool { return a.cm != nil }
	case "edit_scrollback":
		return func() bool { return count() == 2 && len(a.editorFiles) == 1 }
	case "cycle_pane_next", "cycle_pane_previous", "last_pane", "focus_history_back":
		twoPanes(layout.Vertical)
		return func() bool { return a.focusedPane() == first }
	case "focus_history_forward":
		second := twoPanes(layout.Vertical)
		a.focusHistoryStep(false)
		return func() bool { return a.focusedPane() == second }
	case "open_notification_target":
		twoPanes(layout.Vertical)
		a.ShowToast("notice", "target", "info", first)
		return func() bool { return a.focusedPane() == first && a.toast == nil }
	case "previous_agent", "next_agent", "focus_agent":
		second := twoPanes(layout.Vertical)
		a.SetReported(first, "claude", "idle")
		a.SetReported(second, "codex", "idle")
		a.exec(model.CmdFocusPane{Pane: first})
		return func() bool { return a.focusedPane() == second }
	case "navigate_workspace_up", "navigate_workspace_down":
		a.exec(model.CmdNewWorkspace{Name: "second", Cwd: a.cwd})
		a.exec(model.CmdSelectWorkspace{Index: 0})
		a.workspaceNavigate = true
		a.navigateSelected = 0
		return func() bool { return a.navigateSelected == 1 }
	}
	for _, prefix := range []string{"focus_pane_", "navigate_pane_", "swap_pane_"} {
		if strings.HasPrefix(name, prefix) {
			dir := layout.Vertical
			axis := strings.TrimPrefix(name, prefix)
			if axis == "up" || axis == "down" {
				dir = layout.Horizontal
			}
			second := twoPanes(dir)
			start, target := first, second
			if axis == "left" || axis == "up" {
				start, target = second, first
			}
			a.exec(model.CmdFocusPane{Pane: start})
			if prefix == "navigate_pane_" {
				a.workspaceNavigate = true
			}
			if prefix == "swap_pane_" {
				before := fmt.Sprint(tab().Layout.Panes(a.st.PanesArea()))
				return func() bool { return fmt.Sprint(tab().Layout.Panes(a.st.PanesArea())) != before }
			}
			return func() bool { return a.focusedPane() == target }
		}
	}
	t.Fatal("missing action contract", name)
	return func() bool { return false }
}

func TestConfigIndexedBindingsReplaceOriginalInput(t *testing.T) {
	teststate.Lock(t)
	for _, tc := range []struct{ field, old, action string }{{"tabs", "ctrl", "switch_tab"}, {"workspaces", "cmd+shift", "switch_workspace"}, {"agents", "alt", "focus_agent"}} {
		t.Run(tc.field, func(t *testing.T) {
			a, _ := worktreeApp(t)
			p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", "printf READY; exec /bin/cat"})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "READY")
			c, err := config.Decode([]byte(fmt.Sprintf("[keys.indexed]\n%s='ctrl+alt+shift'\n", tc.field)))
			if err != nil || len(c.Warnings) > 0 {
				t.Fatal(c.Warnings, err)
			}
			a.SetSidebarConfig(c, false)
			check := prepareRemapAction(t, a, tc.action)
			feedConfigChord(t, a, tc.old+"+1..9")
			if check() {
				t.Fatal("old indexed key still performs action")
			}
			feedConfigChord(t, a, "ctrl+alt+shift+1..9")
			if !check() {
				t.Fatal("new indexed key did not perform action")
			}
		})
	}
}
