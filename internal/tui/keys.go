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
	case k.Code == 'q':
		a.openQuitDialog()
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
	if ws.Layout.PaneCount() <= 1 {
		if len(a.st.Workspaces) <= 1 {
			a.openQuitDialog()
		} else {
			a.openCloseWorkspaceDialog(a.st.Active)
		}
		return
	}
	a.exec(model.CmdClosePane{Pane: ws.Layout.Focused()})
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
	switch {
	case k.Code == uv.KeyEnter:
		d := a.dialog
		a.dialog = nil
		d.run(a)
	case k.Code == uv.KeyEscape:
		a.dialog = nil
	}
	a.dirty = true
}
