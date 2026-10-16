package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

// isCtrlB detects the prefix key.
func isCtrlB(k uv.Key) bool {
	return k.Code == 'b' && k.Mod.Contains(uv.ModCtrl)
}

// handleKey routes one key event.
func (a *App) handleKey(k uv.Key, raw []byte) {
	if a.dialog != nil && a.dialog.onboarding {
		if k.Code == uv.KeyEnter || k.Code == uv.KeyRight || k.Code == 'l' {
			a.dialog.run(a)
		}
		return
	}
	if a.navigator != nil {
		a.navigatorKey(k)
		return
	}
	if a.settings != nil {
		a.settingsKey(k)
		return
	}
	if a.help != nil {
		a.helpKey(k)
		return
	}
	if a.dialog != nil {
		a.dialogKey(k)
		return
	}
	if a.menu != nil {
		a.menuKey(k)
		return
	}
	if a.prefix {
		a.prefix = false
		a.dirty = true
		if a.prefixMatches(k) {
			if pid := a.focusedPane(); pid >= 0 {
				a.sendPaneKey(k, raw, false)
			}
			return
		}
		if a.sidebarConfig.Keys.Actions != nil {
			a.configuredKey(k, true)
		} else {
			a.prefixKey(k)
		}
		return
	}
	if a.prefixMatches(k) {
		if a.workspaceNavigate {
			a.workspaceNavigate = false
			a.dirty = true
			return
		}
		a.prefix = true
		a.dirty = true
		return
	}
	if a.configuredKey(k, false) {
		return
	}
	if a.st.Mobile() && a.mobileSwitcher {
		if k.Code == uv.KeyEscape {
			a.mobileSwitcher = false
			a.dirty = true
		}
		return
	}
	if a.resizeMode || a.workspaceNavigate {
		a.modeKey(k)
		return
	}
	if a.cm != nil {
		a.copyKey(k, raw)
		return
	}
	pid := a.focusedPane()
	if pid < 0 {
		return
	}
	if a.st.PopupVisible() && k.Code == uv.KeyEscape && k.Mod == 0 {
		if p := a.sup.Get(pid); p != nil && !p.OwnsEscape() {
			a.exec(model.CmdClosePopup{})
			return
		}
	}
	if k.Mod == 0 && (k.Code == uv.KeyPgUp || k.Code == uv.KeyPgDown) {
		if p := a.sup.Get(pid); p != nil {
			sn := p.Snapshot()
			if !sn.AltScreen && !sn.AppCursorKeys {
				offset, height := a.scroll[pid], a.innerRect(pid).H
				if k.Code == uv.KeyPgUp {
					offset = min(offset+height, sn.ScrollbackLen)
				} else {
					offset = max(offset-height, 0)
				}
				a.setScrollOff(pid, offset)
				a.dirty = true
				return
			}
		}
	}
	if a.scroll[pid] > 0 {
		delete(a.scroll, pid) // key input snaps back to the live screen
	}
	a.sendPaneKey(k, raw, false)
	a.dirty = true
}

// prefixKey executes the prefix command table (5.8).
func (a *App) prefixKey(k uv.Key) {
	if k.Mod.Contains(uv.ModAlt) {
		return
	}
	s := a.st
	code := k.Code
	if code >= 'a' && code <= 'z' && k.Mod.Contains(uv.ModShift) {
		code -= 'a' - 'A'
	}
	for _, binding := range a.sidebarConfig.Keys.Command {
		if rune, ok := config.ParseKeyBinding(binding.Key); ok && rune == code && binding.Type == "plugin_action" {
			a.invokePluginAction(binding.Command)
			return
		}
	}
	switch {
	case code == 's':
		a.openSettings()
	case code == 'R':
		a.reloadSidebarConfig()
	case a.renameKeys[code]:
		a.openRenamePaneDialog(a.focusedPane())
	case a.editKeys[code]:
		a.editScrollback()
	case a.historyBackKeys[code]:
		a.focusHistoryStep(false)
	case a.historyForwardKeys[code]:
		a.focusHistoryStep(true)
	case a.navKey != 0 && k.Code == a.navKey:
		// [keys] open_notification_target (default prefix+o).
		a.navToToastTarget()
		a.toast = nil
	case isCtrlB(k):
		// C-b C-b sends C-b to the focused pane.
		if pid := a.focusedPane(); pid >= 0 {
			a.sup.Input(pid, []byte{0x02})
		}
	case a.copyKeys[k.Code]:
		// [keys] copy_mode (default prefix+[ and prefix+]).
		a.enterCopy(false)
	case k.Code == '?':
		a.openHelp()
	case a.splitVKeys[k.Code]:
		// [keys] split_vertical (default prefix+% and prefix+v).
		a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	case a.splitHKeys[k.Code]:
		// [keys] split_horizontal (default prefix+" and prefix+minus).
		a.exec(model.CmdSplitFocused{Dir: layout.Horizontal})
	case code == 'g':
		a.openNavigator()
	case code == 'r':
		a.resizeMode = true
	case code == 'w':
		a.workspaceNavigate = true
		a.navigateHint = true
		a.navigateSelected = s.Active
	case code == 'W':
		a.renameWorkspaceDialog(s.Active)
	case code == 'T':
		if ws := s.ActiveWorkspace(); ws != nil {
			a.openRenameTabDialog(s.Active, ws.ActiveTab)
		}
	case code == 'X':
		a.closeActiveTab()
	case code == 'G':
		a.newWorktreeDialog(s.Active)
	case code == 'z':
		a.exec(model.CmdZoomPane{Pane: a.focusedPane(), Mode: "toggle"})
	case code == 'x':
		a.closeFocusedPane()
	case k.Code == 'h' || k.Code == uv.KeyLeft:
		a.exec(model.CmdFocusDir{Dir: layout.NavLeft})
	case k.Code == 'j' || k.Code == uv.KeyDown:
		a.exec(model.CmdFocusDir{Dir: layout.NavDown})
	case k.Code == 'k' || k.Code == uv.KeyUp:
		a.exec(model.CmdFocusDir{Dir: layout.NavUp})
	case k.Code == 'l' || k.Code == uv.KeyRight:
		a.exec(model.CmdFocusDir{Dir: layout.NavRight})
	case k.Code == uv.KeyTab && k.Mod.Contains(uv.ModShift):
		a.exec(model.CmdCyclePane{Next: false})
	case k.Code == uv.KeyTab:
		a.exec(model.CmdCyclePane{Next: true})
	case k.Code == 'N' || (k.Code == 'n' && k.Mod.Contains(uv.ModShift)):
		a.newWorkspace()
	case k.Code == 'D' || (k.Code == 'd' && k.Mod.Contains(uv.ModShift)):
		a.openCloseWorkspaceDialog(s.Active)
	case code == 'n':
		a.cycleTab(1)
	case code == 'p':
		a.cycleTab(-1)
	case k.Code >= '1' && k.Code <= '9':
		if ws := s.ActiveWorkspace(); ws != nil {
			index := int(k.Code - '1')
			if index < len(ws.Tabs) {
				a.exec(model.CmdSelectTab{Workspace: s.Active, Tab: index})
			}
		}
	case k.Code == 'b':
		if s.Mobile() {
			a.mobileSwitcher = !a.mobileSwitcher
			if a.mobileSwitcher {
				a.mobileScroll = 0
			}
			a.dirty = true
		} else {
			a.exec(model.CmdToggleSidebar{})
		}
	case code == 'c':
		a.newTabNameDialog()
	case code == 'q' && k.Mod == 0:
		if a.detacher != nil {
			a.detacher("detach requested")
		}

	}
}

// cycleTab moves the active tab of the active workspace by dir, wrapping.
func (a *App) cycleTab(dir int) {
	ws := a.st.ActiveWorkspace()
	if ws == nil || len(ws.Tabs) < 2 {
		return
	}
	a.exec(model.CmdSelectTab{
		Workspace: a.st.Active,
		Tab:       (ws.ActiveTab + dir + len(ws.Tabs)) % len(ws.Tabs),
	})
}

// closeActiveTab closes the focused tab of the active workspace.
func (a *App) closeActiveTab() {
	if ws := a.st.ActiveWorkspace(); ws != nil {
		a.closeTabAt(a.st.Active, ws.ActiveTab)
	}
}

// selectWorkspace wraps indices.
func (a *App) selectWorkspace(i int) {
	n := len(a.st.Workspaces)
	if n == 0 {
		return
	}
	i = ((i % n) + n) % n
	a.exec(model.CmdSelectWorkspace{Index: i})
}

// newWorkspace creates a workspace in the focused pane's cwd.
func (a *App) newWorkspace() {
	cwd := a.cwd
	if ws := a.st.ActiveWorkspace(); ws != nil {
		cwd = ws.Cwd
		if cached := a.sessionCwds[ws.ID]; cached != "" {
			cwd = cached
		}
	}
	cwd = a.newPaneCwd(cwd)
	a.exec(model.CmdNewWorkspace{Cwd: cwd, Name: filepath.Base(cwd)})
}

// closeFocusedPane replaces the final workspace's shell, or confirms closing
// another workspace when its final pane is selected.
func (a *App) closeFocusedPane() {
	if a.st.OverlayVisible() {
		a.exec(model.CmdCloseOverlay{})
		return
	}
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return
	}
	tab := ws.TabRef()
	if tab == nil {
		return
	}
	if tab.Layout.PaneCount() <= 1 && len(ws.Tabs) <= 1 {
		if len(a.st.Workspaces) <= 1 {
			a.replaceLastWorkspace()
		} else {
			a.openCloseWorkspaceDialog(a.st.Active)
		}
		return
	}
	a.exec(model.CmdClosePane{Pane: tab.Layout.Focused()})
}

func (a *App) replaceLastWorkspace() {
	old := a.st.Active
	a.lastSpawnError = nil
	a.newWorkspace()
	if a.lastSpawnError != nil {
		a.exec(model.CmdCloseWorkspace{Index: a.st.Active})
		return
	}
	a.exec(model.CmdCloseWorkspace{Index: old})
}

// --- dialogs ---

func (a *App) openQuitDialog() {
	a.workspaceNavigate = false
	a.navigateHint = false
	a.dialog = &uiDialog{
		d: render.Dialog{Title: "Stop server?", Detail: "Stops the gtx server and all running panes.", Confirm: "stop", Cancel: "cancel", Width: 76, CenterTerminal: a.serverUI},
		run: func(a *App) {
			a.exec(model.CmdQuit{})
		},
		cancel: func(a *App) {
			a.workspaceNavigate = true
			a.navigateSelected = a.st.Active
		},
	}
	a.dirty = true
}

func (a *App) openCloseWorkspaceDialog(idx int) {
	if idx < 0 || idx >= len(a.st.Workspaces) {
		return
	}
	if a.sidebarConfig.UI.ConfirmClose != nil && !*a.sidebarConfig.UI.ConfirmClose {
		a.exec(model.CmdCloseWorkspace{Index: idx})
		return
	}
	ws := a.st.Workspaces[idx]
	name := ws.Name
	if ws.DefaultName {
		name = render.WorkspaceDisplayName(ws, a.paneSnaps())
	}
	if !ws.CustomName && !ws.DefaultName {
		name = ws.Cwd
		if cwd := a.sessionCwds[ws.ID]; cwd != "" {
			name = cwd
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if resolved, err := filepath.EvalSymlinks(home); err == nil {
				home = resolved
			}
			if resolved, err := filepath.EvalSymlinks(name); err == nil {
				name = resolved
			}
			if name == home {
				name = "~"
			} else if strings.HasPrefix(name, home+"/") {
				name = "~" + strings.TrimPrefix(name, home)
			}
		}
	}
	panes := ws.TabRef().Layout.PaneCount()
	noun := "panes"
	if panes == 1 {
		noun = "pane"
	}
	a.workspaceNavigate = false
	a.navigateHint = false
	a.dialog = &uiDialog{
		d: render.Dialog{Title: "Close workspace?", Detail: fmt.Sprintf("%s — %d %s", name, panes, noun), Lines: []string{""}, Width: 64, DimPanesOnly: true, Confirm: "confirm", Cancel: "cancel"},
		run: func(a *App) {
			a.exec(model.CmdCloseWorkspace{Index: idx})
		},
		cancel: func(a *App) {
			a.workspaceNavigate = true
			a.navigateSelected = idx
			if a.st.Mobile() && !a.serverUI {
				a.mobileSwitcher = true
				a.mobileScroll = 0
			}
		},
	}
	a.dirty = true
}

func (a *App) cancelDialog() {
	d := a.dialog
	a.dialog = nil
	if d.cancel != nil {
		d.cancel(a)
	}
}

func (a *App) dialogKey(k uv.Key) {
	d := a.dialog
	switch {
	case k.Code == uv.KeyEnter:
		a.dialog = nil
		d.run(a)
	case k.Code == uv.KeyEscape:
		a.cancelDialog()
	case k.Code == uv.KeyTab && k.Mod.Contains(uv.ModShift) && d.back != nil:
		d.back()
	case d.input != nil:
		if d.machineForm {
			a.machineInputKey(k)
			break
		}
		runes := []rune(*d.input)
		switch {
		case k.Code == 'c' && k.Mod.Contains(uv.ModCtrl):
			runes = nil
			d.replaceDefault = false
		case k.Code == uv.KeyBackspace:
			d.replaceDefault = false
			if len(runes) > 0 {
				runes = runes[:len(runes)-1]
			}
		case k.Text != "":
			if d.replaceDefault {
				runes = nil
				d.replaceDefault = false
			}
			runes = append(runes, []rune(k.Text)...)
		}
		*d.input = string(runes)
		d.d.Input, d.d.InputCursor = *d.input, len(runes)
	}
	a.dirty = true
}

// openRenameTabDialog prompts for a new name for workspace ws's tab ti.
func (a *App) openRenameTabDialog(ws, ti int) {
	if ws < 0 || ws >= len(a.st.Workspaces) {
		return
	}
	w := a.st.Workspaces[ws]
	if ti < 0 || ti >= len(w.Tabs) {
		return
	}
	name := w.Tabs[ti].Name
	defaultName := fmt.Sprint(ti + 1)
	if name == "" {
		name = defaultName
	}
	a.dialog = &uiDialog{
		input: &name,
		d: render.Dialog{
			Title:   "Rename tab",
			Confirm: "rename", Cancel: "cancel",
			Input: name, InputCursor: len([]rune(name)), InputEdit: true,
		},
		run: func(a *App) {
			if name != "" && name != w.Tabs[ti].Name && !(w.Tabs[ti].Name == "" && name == defaultName) {
				a.exec(model.CmdRenameTab{Workspace: ws, Tab: ti, Label: name})
			}
		},
	}
	a.dirty = true
}
