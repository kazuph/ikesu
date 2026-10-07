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
		return
	}
	if isCtrlB(k) {
		a.prefix = true
		a.dirty = true
		return
	}
	pid := a.focusedPane()
	if pid < 0 {
		return
	}
	if a.scroll[pid] > 0 {
		delete(a.scroll, pid) // key input snaps back to the live screen
	}
	a.sup.Input(pid, raw)
	a.dirty = true
}

// prefixKey executes the prefix command table (5.8).
func (a *App) prefixKey(k uv.Key) {
	s := a.st
	switch {
	case isCtrlB(k):
		// C-b C-b sends C-b to the focused pane.
		if pid := a.focusedPane(); pid >= 0 {
			a.sup.Input(pid, []byte{0x02})
		}
	case k.Code == 'v':
		a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	case k.Code == '-':
		a.exec(model.CmdSplitFocused{Dir: layout.Horizontal})
	case k.Code == 'x':
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
	case k.Code == 'n':
		a.selectWorkspace(s.Active + 1)
	case k.Code == 'p':
		a.selectWorkspace(s.Active - 1)
	case k.Code >= '1' && k.Code <= '9':
		a.selectWorkspace(int(k.Code - '1'))
	case k.Code == 'b':
		a.exec(model.CmdToggleSidebar{})
	case k.Code == 'c':
		a.exec(model.CmdNewTab{Workspace: s.Active})
	case k.Code == ']':
		a.cycleTab(1)
	case k.Code == '[':
		a.cycleTab(-1)
	case k.Code == ',':
		if ws := s.ActiveWorkspace(); ws != nil {
			a.openRenameTabDialog(s.Active, ws.ActiveTab)
		}
	case k.Code == '&':
		a.closeActiveTab()
	case k.Code == 'd' && k.Mod == 0:
		// Detach: only meaningful when running under a server.
		if a.detacher != nil {
			a.detacher("detach requested")
		}
	case k.Code == 'q':
		a.openQuitDialog()
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
	if p := a.sup.Get(a.focusedPane()); p != nil {
		if c := p.SpawnCwd(); c != "" {
			cwd = c
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
			a.openQuitDialog()
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
		d: render.Dialog{Title: "Quit goterrace?", Detail: "Stops all panes in every workspace.", Confirm: "quit", Cancel: "cancel"},
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
	case d.input != nil && (k.Code == uv.KeyBackspace || k.Code == uv.KeyDelete):
		if r := []rune(*d.input); len(r) > 0 {
			*d.input = string(r[:len(r)-1])
			a.dialog.d.Input = *d.input
		}
	case d.input != nil && k.Text != "":
		*d.input += k.Text
		a.dialog.d.Input = *d.input
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
			Input: name, InputEdit: true,
		},
		run: func(a *App) {
			if name != "" && name != w.Tabs[ti].Name {
				a.exec(model.CmdRenameTab{Workspace: ws, Tab: ti, Label: name})
			}
		},
	}
	a.dirty = true
}
