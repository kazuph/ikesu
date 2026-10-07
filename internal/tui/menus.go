package tui

import (
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

// openPaneMenu shows the per-pane right-click menu at (x, y).
func (a *App) openPaneMenu(x, y, pid int) {
	a.menu = &uiMenu{justOpened: true,
		kind: menuPane,
		pane: pid,
		m: render.Menu{
			X: x, Y: y, Hover: -1, Sel: -1,
			Items: []render.MenuItem{
				{Label: "Split vertical"},
				{Label: "Split horizontal"},
				{Separator: true},
				{Label: "Close pane"},
			},
		},
	}
}

// openWorkspaceMenu shows the workspace menu (clicking the active row).
func (a *App) openWorkspaceMenu(x, y, ws int) {
	a.menu = &uiMenu{justOpened: true,
		kind: menuWorkspace,
		ws:   ws,
		m: render.Menu{
			X: x, Y: y, Hover: -1, Sel: -1,
			Items: []render.MenuItem{
				{Label: "New workspace"},
				{Separator: true},
				{Label: "Close workspace…"},
			},
		},
	}
}

// openTabMenu shows the right-click menu for a tab cell.
func (a *App) openTabMenu(x, y, ws, ti int) {
	a.menu = &uiMenu{justOpened: true,
		kind: menuTab,
		ws:   ws, tab: ti,
		m: render.Menu{
			X: x, Y: y, Hover: -1, Sel: -1,
			Items: []render.MenuItem{
				{Label: "Rename tab…"},
				{Label: "New tab"},
				{Separator: true},
				{Label: "Close tab"},
			},
		},
	}
}

// openMainMenu shows the [menu] global menu.
func (a *App) openMainMenu(x, y int) {
	sideLabel := "Hide sidebar"
	if !a.st.SidebarVisible {
		sideLabel = "Show sidebar"
	}
	a.menu = &uiMenu{justOpened: true,
		kind: menuMain,
		m: render.Menu{
			X: x, Y: y, Hover: -1, Sel: -1,
			Items: []render.MenuItem{
				{Label: "New workspace"},
				{Label: "Next workspace"},
				{Label: "Previous workspace"},
				{Separator: true},
				{Label: sideLabel},
				{Separator: true},
				{Label: "Restart server…", Disabled: a.restartHook == nil},
				{Label: "Quit goterrace…"},
			},
		},
	}
}

// activateMenu runs the command for menu item i and closes the menu.
func (a *App) activateMenu(i int) {
	m := a.menu
	a.menu = nil
	if m == nil || i < 0 || i >= len(m.m.Items) || m.m.Items[i].Separator || m.m.Items[i].Disabled {
		return
	}
	switch m.kind {
	case menuPane:
		pid := m.pane
		switch i {
		case 0:
			a.exec(model.CmdFocusPane{Pane: pid})
			a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
		case 1:
			a.exec(model.CmdFocusPane{Pane: pid})
			a.exec(model.CmdSplitFocused{Dir: layout.Horizontal})
		case 3:
			a.exec(model.CmdFocusPane{Pane: pid})
			a.closeFocusedPane()
		}
	case menuWorkspace:
		switch i {
		case 0:
			a.newWorkspace()
		case 2:
			a.openCloseWorkspaceDialog(m.ws)
		}
	case menuTab:
		switch i {
		case 0:
			a.openRenameTabDialog(m.ws, m.tab)
		case 1:
			a.exec(model.CmdNewTab{Workspace: m.ws})
		case 3:
			a.closeTabAt(m.ws, m.tab)
		}
	case menuMain:
		switch i {
		case 0:
			a.newWorkspace()
		case 1:
			a.selectWorkspace(a.st.Active + 1)
		case 2:
			a.selectWorkspace(a.st.Active - 1)
		case 4:
			a.exec(model.CmdToggleSidebar{})
		case 6:
			a.openRestartDialog()
		case 7:
			a.openQuitDialog()
		}
	}
}

// menuItemAt returns the item index under (x, y) or -1.
func (a *App) menuItemAt(x, y int) int {
	if a.menu == nil {
		return -1
	}
	r := render.MenuRect(&a.menu.m, a.st.Cols, a.st.Rows)
	i := y - r.Y - 1
	if x < r.X || x >= r.X+r.W || i < 0 || i >= len(a.menu.m.Items) {
		return -1
	}
	return i
}

// menuMouse handles hover and click/release inside a menu.
func (a *App) menuMouse(ev input.Event) {
	m := ev.Mouse
	i := a.menuItemAt(m.X, m.Y)
	switch ev.Kind {
	case input.KindMouseMotion:
		if i >= 0 && !a.menu.m.Items[i].Separator {
			a.menu.m.Hover = i
			a.menu.m.Sel = i
		} else {
			a.menu.m.Hover = -1
		}
	case input.KindMouseClick:
		if i < 0 {
			a.menu = nil
		}
	case input.KindMouseRelease:
		if a.menu.justOpened {
			a.menu.justOpened = false
			return
		}
		if i >= 0 {
			a.activateMenu(i)
		}
		// Releasing outside the menu keeps it open; clicks (press) outside
		// close it, matching click-to-dismiss.
	}
}

// menuKey handles Up/Down/Enter/Esc while a menu is open.
func (a *App) menuKey(k uv.Key) {
	m := a.menu
	if m == nil {
		return
	}
	step := func(dir int) {
		n := len(m.m.Items)
		sel := m.m.Sel
		for j := 0; j < n; j++ {
			sel = (sel + dir + n) % n
			if !m.m.Items[sel].Separator && !m.m.Items[sel].Disabled {
				m.m.Sel = sel
				return
			}
		}
	}
	switch {
	case k.Code == uv.KeyEscape:
		a.menu = nil
	case k.Code == uv.KeyUp:
		if m.m.Sel < 0 {
			m.m.Sel = len(m.m.Items)
		}
		step(-1)
	case k.Code == uv.KeyDown:
		if m.m.Sel < 0 {
			m.m.Sel = -1
		}
		step(1)
	case k.Code == uv.KeyEnter:
		if m.m.Sel >= 0 {
			a.activateMenu(m.m.Sel)
		}
	}
	a.dirty = true
}

// dialogMouse resolves clicks on the confirmation dialog.
func (a *App) dialogMouse(ev input.Event) {
	if ev.Kind != input.KindMouseClick && ev.Kind != input.KindMouseRelease {
		return
	}
	m := ev.Mouse
	outer, cancel, confirm := render.DialogRects(&a.dialog.d, a.st.Cols, a.st.Rows)
	switch {
	case pointIn(m.X, m.Y, confirm):
		if ev.Kind == input.KindMouseClick {
			d := a.dialog
			a.dialog = nil
			d.run(a)
		}
	case pointIn(m.X, m.Y, cancel):
		a.dialog = nil
	case !pointIn(m.X, m.Y, outer):
		if ev.Kind == input.KindMouseClick {
			a.dialog = nil
		}
	}
}
