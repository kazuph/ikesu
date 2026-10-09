package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func (a *App) modeHint() string {
	if a.prefix {
		return " PREFIX  esc cancel  ctrl+b send prefix  w workspace nav  ? keybinds"
	}
	if a.resizeMode {
		return " RESIZE   h/l width  j/k height  esc done"
	}
	if a.workspaceNavigate {
		return " NAVIGATE  esc back  up / down ws  ⇥ pane  g navigator  c new tab  v split│  - split─  x close  z zoom  r resize  ? keybinds  s settings"
	}
	return ""
}

func (a *App) modeKey(k uv.Key) {
	if k.Code == uv.KeyEscape {
		a.resizeMode, a.workspaceNavigate = false, false
		a.dirty = true
		return
	}
	if a.resizeMode {
		var dir layout.NavDir
		switch k.Code {
		case 'h', uv.KeyLeft:
			dir = layout.NavLeft
		case 'l', uv.KeyRight:
			dir = layout.NavRight
		case 'j', uv.KeyDown:
			dir = layout.NavDown
		case 'k', uv.KeyUp:
			dir = layout.NavUp
		default:
			return
		}
		// The existing layout default is the observed 0.05 ratio step.
		a.exec(model.CmdResizePane{Pane: a.focusedPane(), Dir: dir})
		return
	}
	switch {
	case k.Code == uv.KeyUp || k.Code == uv.KeyDown:
		order := a.st.VisibleWorkspaceOrder()
		if len(order) == 0 {
			return
		}
		at := 0
		for i, index := range order {
			if index == a.navigateSelected {
				at = i
				break
			}
		}
		delta := 1
		if k.Code == uv.KeyUp {
			delta = -1
		}
		a.navigateSelected = order[(at+delta+len(order))%len(order)]
	case k.Code == uv.KeyEnter:
		a.exec(model.CmdSelectWorkspace{Index: a.navigateSelected})
		a.workspaceNavigate = false
	case k.Code >= '1' && k.Code <= '9':
		index := int(k.Code - '1')
		if index < len(a.st.Workspaces) {
			a.exec(model.CmdSelectWorkspace{Index: index})
			a.workspaceNavigate = false
		}
	case k.Code == 'g':
		a.openNavigator()
	case k.Code == 'c' || k.Code == 'v':
		a.workspaceNavigate = false
		a.prefixKey(k)
	default:
		a.prefixKey(k)
	}
	a.dirty = true
}
