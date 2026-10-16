package tui

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/childenv"
	"os"
	"path/filepath"
	"sort"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

func (a *App) prefixMatches(k uv.Key) bool {
	spec := a.sidebarConfig.Keys.Prefix
	if spec == "" {
		spec = "ctrl+b"
	}
	chord, err := config.ParseChord(spec)
	return err == nil && chord.Match(k, false)
}
func (a *App) configuredKey(k uv.Key, prefix bool) bool {
	// Explicit custom commands take precedence over the built-in action using
	// the same chord (the user's prefix+g intentionally replaces goto).
	for _, binding := range a.sidebarConfig.Keys.Command {
		chord, err := config.ParseChord(binding.Key)
		if err == nil && chord.Match(k, prefix) {
			a.runKeyCommand(binding)
			return true
		}
	}
	names := make([]string, 0, len(config.ActionDefaults))
	for name := range config.ActionDefaults {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.HasPrefix(name, "navigate_") || (name == "remote_image_paste" || name == "fullscreen") {
			continue
		}
		for _, spec := range a.sidebarConfig.ActionBindings(name) {
			if spec == "" {
				continue
			}
			chord, err := config.ParseChord(spec)
			if err == nil && chord.Match(k, prefix) {
				return a.configAction(name, k)
			}
		}
	}
	return false
}
func (a *App) configAction(name string, k uv.Key) bool {
	switch name {
	case "swap_pane_left", "swap_pane_down", "swap_pane_up", "swap_pane_right":
		dirs := map[string]layout.NavDir{"swap_pane_left": layout.NavLeft, "swap_pane_down": layout.NavDown, "swap_pane_up": layout.NavUp, "swap_pane_right": layout.NavRight}
		a.exec(model.CmdSwapDir{Pane: a.focusedPane(), Dir: dirs[name]})
	case "previous_workspace":
		a.cycleWorkspace(-1)
	case "next_workspace":
		a.cycleWorkspace(1)
	case "switch_workspace":
		order := a.st.VisibleWorkspaceOrder()
		i := int(k.Code - '1')
		if i >= 0 && i < len(order) {
			a.exec(model.CmdSelectWorkspace{Index: order[i]})
		}
	case "switch_tab":
		if w := a.st.ActiveWorkspace(); w != nil {
			i := int(k.Code - '1')
			if i >= 0 && i < len(w.Tabs) {
				a.exec(model.CmdSelectTab{Workspace: a.st.Active, Tab: i})
			}
		}
	case "last_pane":
		a.configFocusPane(a.lastPane)
	case "focus_history_back":
		a.focusHistoryStep(false)
	case "focus_history_forward":
		a.focusHistoryStep(true)
	case "rename_pane":
		a.openRenamePaneDialog(a.focusedPane())
	case "edit_scrollback":
		a.editScrollback()
	case "copy_mode":
		a.enterCopy(false)
	case "split_vertical":
		a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	case "split_horizontal":
		a.exec(model.CmdSplitFocused{Dir: layout.Horizontal})
	case "open_notification_target":
		a.openToastTarget()
	case "new_tab":
		if a.sidebarConfig.UI.PromptNewTabName != nil && !*a.sidebarConfig.UI.PromptNewTabName {
			a.exec(model.CmdNewTab{Workspace: a.st.Active})
		} else {
			a.newTabNameDialog()
		}
	case "open_worktree":
		a.openWorktreeMenu(a.st.Active)
	case "remove_worktree":
		a.deleteWorktreeDialog(a.st.Active, false)
	case "previous_agent", "next_agent", "focus_agent":
		a.configAgentFocus(name, k)
	default:
		codes := map[string]rune{"help": '?', "settings": 's', "detach": 'q', "reload_config": 'R', "workspace_picker": 'w', "goto": 'g', "new_workspace": 'N', "new_worktree": 'G', "rename_workspace": 'W', "close_workspace": 'D', "rename_tab": 'T', "previous_tab": 'p', "next_tab": 'n', "close_tab": 'X', "focus_pane_left": 'h', "focus_pane_down": 'j', "focus_pane_up": 'k', "focus_pane_right": 'l', "cycle_pane_next": uv.KeyTab, "cycle_pane_previous": uv.KeyTab, "close_pane": 'x', "zoom": 'z', "resize_mode": 'r', "toggle_sidebar": 'b'}
		code, ok := codes[name]
		if !ok {
			return false
		}
		key := uv.Key{Code: code}
		if name == "cycle_pane_previous" {
			key.Mod = uv.ModShift
		}
		a.prefixKey(key)
	}
	return true
}
func (a *App) runKeyCommand(binding config.PluginKeyBinding) {
	cwd := a.cwd
	if w := a.st.ActiveWorkspace(); w != nil {
		cwd = w.Cwd
	}
	if p := a.sup.Get(a.focusedPane()); p != nil {
		if current := p.Snapshot().Cwd; current != "" {
			cwd = current
		} else {
			cwd = p.SpawnCwd()
		}
	}
	shell := "/bin/sh"
	argv := []string{shell, "-c", binding.Command}
	var err error
	switch binding.Type {
	case "plugin_action":
		a.invokePluginAction(binding.Command)
	case "popup":
		_, err = a.OpenPluginPopup(cwd, argv, a.keyCommandEnv(cwd), binding.Width.Size, binding.Height.Size)
	case "pane":
		_, err = a.OpenPluginPane(a.st.Active, a.focusedPane(), "zoomed", "", cwd, argv, a.keyCommandEnv(cwd), true)
	case "shell":
		cmd := childenv.Command(shell, "-lc", binding.Command)
		cmd.Dir = cwd
		cmd.Env = childenv.Scrub(os.Environ())
		for key, value := range a.keyCommandEnv(cwd) {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		if err = cmd.Start(); err == nil {
			go func() { _ = cmd.Wait() }()
		}
	}
	if err != nil {
		a.worktreeError(err)
	}
}
func (a *App) keyCommandEnv(cwd string) map[string]string {
	bin, _ := os.Executable()
	env := map[string]string{"GTX_BIN_PATH": bin, "GTX_ACTIVE_PANE_CWD": cwd, "GTX_ACTIVE_PANE_ID": fmt.Sprintf("p%d", a.focusedPane())}
	if socket := os.Getenv("GTX_SOCKET_PATH"); socket != "" {
		env["GTX_SOCKET_PATH"] = socket
	}
	if w := a.st.ActiveWorkspace(); w != nil {
		env["GTX_ACTIVE_WORKSPACE_ID"] = fmt.Sprintf("s%d", w.ID)
		if t := w.TabRef(); t != nil {
			env["GTX_ACTIVE_TAB_ID"] = fmt.Sprintf("s%d:t%d", w.ID, t.ID)
		}
	}
	return env
}

func (a *App) configFocusPane(pid int) {
	for wi, w := range a.st.Workspaces {
		for ti, t := range w.Tabs {
			if t.Layout.HasPane(pid) {
				a.exec(model.CmdSelectWorkspace{Index: wi})
				a.exec(model.CmdSelectTab{Workspace: wi, Tab: ti})
				a.exec(model.CmdFocusPane{Pane: pid})
				return
			}
		}
	}
}
func (a *App) configAgentFocus(name string, k uv.Key) {
	ids := []int{}
	for _, row := range render.SidebarRows(a.st, a.paneSnaps(), a.sidebarConfig) {
		if row.Kind == render.RowAgent && row.Line == 0 {
			ids = append(ids, row.Pane)
		}
	}
	if len(ids) == 0 {
		return
	}
	at := -1
	for i, pid := range ids {
		if pid == a.focusedPane() {
			at = i
			break
		}
	}
	switch name {
	case "focus_agent":
		at = int(k.Code - '1')
		if at < 0 || at >= len(ids) {
			return
		}
	case "next_agent":
		at = (at + 1) % len(ids)
	case "previous_agent":
		if at < 0 {
			at = 0
		}
		at = (at + len(ids) - 1) % len(ids)
	}
	a.configFocusPane(ids[at])
}

func (a *App) newPaneCwd(source string) string {
	switch policy := a.sidebarConfig.Terminal.NewCwd; policy {
	case "", "follow":
		return source
	case "current":
		return a.cwd
	case "home":
		home, _ := os.UserHomeDir()
		return home
	default:
		if policy == "~" || strings.HasPrefix(policy, "~/") {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, strings.TrimPrefix(policy, "~/"))
		}
		if !filepath.IsAbs(policy) {
			return filepath.Join(a.cwd, policy)
		}
		return policy
	}
}
