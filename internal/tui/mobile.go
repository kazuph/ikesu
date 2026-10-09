package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

func (a *App) mobileMouse(ev input.Event) bool {
	if !a.st.Mobile() {
		return false
	}
	m := ev.Mouse
	if pointIn(m.X, m.Y, render.MobileSwitchRect(a.st)) {
		if ev.Kind == input.KindMouseClick && m.Button == uv.MouseLeft {
			a.mobileSwitcher = !a.mobileSwitcher
			if a.mobileSwitcher {
				a.mobileScroll = 0
			}
		}
		return true
	}
	if !a.mobileSwitcher {
		return m.Y < 2
	}
	if ev.Kind == input.KindMouseWheel {
		if m.Button == uv.MouseWheelUp {
			a.mobileScroll -= 2
		} else if m.Button == uv.MouseWheelDown {
			a.mobileScroll += 2
		}
		a.mobileScroll = render.MobileOffset(a.st, a.mainMenuItems(), a.mobileScroll)
	}
	if ev.Kind == input.KindMouseClick && m.Button == uv.MouseLeft {
		viewport := render.MobileViewport(a.st)
		if pointIn(m.X, m.Y, viewport) {
			rows := render.MobileRows(a.st, a.mainMenuItems())
			index := m.Y - viewport.Y + render.MobileOffset(a.st, a.mainMenuItems(), a.mobileScroll)
			if index < len(rows) {
				row := rows[index]
				switch row.Kind {
				case render.RowMobileNewWorkspace:
					a.mobileSwitcher = false
					a.newWorkspace()
				case render.RowMobileNewTab:
					a.mobileSwitcher = false
					a.newTabNameDialog()
				case render.RowMobileTab:
					a.mobileSwitcher = false
					a.exec(model.CmdSelectTab{Workspace: a.st.Active, Tab: row.Line})
				case render.RowMobileMenuItem:
					item := a.mainMenuItems()[row.Line]
					if !item.Separator && !item.Disabled {
						a.mobileSwitcher = false
						a.openMainMenu(0, 0)
						a.activateMenu(row.Line)
					}
				case render.RowSection:
					a.toggleSection(row.Section)
				case render.RowWorkspace, render.RowGit:
					a.exec(model.CmdSelectWorkspace{Index: row.Ws})
					a.mobileSwitcher = false
				}
			}
		}
	}
	return true
}
