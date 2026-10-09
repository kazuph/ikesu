package tui

import (
	uv "github.com/charmbracelet/ultraviolet"

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
		a.prefixKey(k)
		// A prefix command run inside copy mode that moved the focus
		// elsewhere closes the copy view.
		if a.cm != nil && a.cm.pane != a.focusedPane() {
			a.exitCopy()
		}
		return
	}
	if isCtrlB(k) {
		a.prefix = true
		a.dirty = true
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
	if a.st.PopupVisible() && k.Code == uv.KeyEscape {
		a.exec(model.CmdClosePopup{})
		return
	}
	pid := a.focusedPane()
	if pid < 0 {
		return
	}
	if a.scroll[pid] > 0 {
		delete(a.scroll, pid) // key input snaps back to the live screen
	}
	if k.Code == uv.KeyEnter && k.Mod == uv.ModShift {
		// FixTerm Shift+Enter arrives as the legacy sequence
		// CSI 27;2;13~ (or kitty CSI 13;2u). Per SPEC G6 the pane
		// receives a bare LF — unless the app explicitly pushed the
		// kitty keyboard protocol, which takes over the key itself.
		if p := a.sup.Get(pid); p != nil && !p.KbdExplicit() {
			a.sup.Input(pid, []byte{'\n'})
			a.dirty = true
			return
		}
	}
	a.sup.Input(pid, raw)
	a.dirty = true
}

// prefixKey executes the prefix command table (5.8).
func (a *App) prefixKey(k uv.Key) {
	s := a.st
	code := k.Code
	if code >= 'a' && code <= 'z' && k.Mod.Contains(uv.ModShift) {
		code -= 'a' - 'A'
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
	case code == 'r':
		a.resizeMode = true
	case code == 'w':
		a.workspaceNavigate = true
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
	a.exec(model.CmdNewWorkspace{Cwd: cwd})
}

// closeFocusedPane closes the focused pane; on the last pane of a workspace
// it asks the close-workspace dialog (or quit dialog on the last workspace).
func (a *App) closeFocusedPane() {
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
			old := tab.Layout.Focused()
			a.lastSpawnError = nil
			a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
			if a.lastSpawnError != nil {
				a.exec(model.CmdClosePane{Pane: tab.Layout.Focused()})
				return
			}
			a.exec(model.CmdClosePane{Pane: old})
		} else {
			a.openCloseWorkspaceDialog(a.st.Active)
		}
		return
	}
	a.exec(model.CmdClosePane{Pane: tab.Layout.Focused()})
}

// --- dialogs ---

func (a *App) openQuitDialog() {
	a.dialog = &uiDialog{
		d: render.Dialog{Title: "Stop server?", Detail: "Stops the gtx server and all running panes.", Confirm: "stop", Cancel: "cancel", Width: 76},
		run: func(a *App) {
			a.exec(model.CmdQuit{})
		},
	}
	a.dirty = true
}

func (a *App) openCloseWorkspaceDialog(idx int) {
	if idx < 0 || idx >= len(a.st.Workspaces) {
		return
	}
	name := a.st.Workspaces[idx].Name
	a.dialog = &uiDialog{
		d: render.Dialog{Title: "Close workspace?", Detail: "Closes " + name + " and all of its panes.", Confirm: "close", Cancel: "cancel"},
		run: func(a *App) {
			a.exec(model.CmdCloseWorkspace{Index: idx})
		},
	}
	a.dirty = true
}

func (a *App) dialogKey(k uv.Key) {
	d := a.dialog
	switch {
	case k.Code == uv.KeyEnter:
		a.dialog = nil
		d.run(a)
	case k.Code == uv.KeyEscape:
		a.dialog = nil
	case d.input != nil:
		runes := []rune(*d.input)
		switch {
		case k.Code == 'c' && k.Mod.Contains(uv.ModCtrl):
			runes = nil
		case k.Code == uv.KeyBackspace:
			if len(runes) > 0 {
				runes = runes[:len(runes)-1]
			}
		case k.Text != "":
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
	a.dialog = &uiDialog{
		input: &name,
		d: render.Dialog{
			Title:   "Rename tab",
			Confirm: "rename", Cancel: "cancel",
			Input: name, InputCursor: len([]rune(name)), InputEdit: true,
		},
		run: func(a *App) {
			if name != "" && name != w.Tabs[ti].Name {
				a.exec(model.CmdRenameTab{Workspace: ws, Tab: ti, Label: name})
			}
		},
	}
	a.dirty = true
}
