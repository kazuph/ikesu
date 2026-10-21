package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
)

func (a *App) modeHint() string {
	if a.prefix {
		return " PREFIX  esc cancel  " + a.prefixSpec() + " send prefix  w workspace nav  ? keybinds"
	}
	if a.resizeMode {
		return " RESIZE   h/l width  j/k height  esc done"
	}
	if a.workspaceNavigate && (!a.serverUI || a.navigateHint) {
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
		if k.Code == uv.KeyEnter || a.resizeExitKey(k) {
			a.resizeMode = false
			a.dirty = true
			return
		}
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
	if a.sidebarConfig.Keys.Actions != nil {
		for name, replacement := range map[string]rune{"navigate_workspace_up": uv.KeyUp, "navigate_workspace_down": uv.KeyDown, "navigate_pane_left": 'h', "navigate_pane_down": 'j', "navigate_pane_up": 'k', "navigate_pane_right": 'l'} {
			for _, spec := range a.sidebarConfig.ActionBindings(name) {
				if spec != "" {
					chord, err := config.ParseChord(spec)
					if err == nil && chord.Match(k, false) {
						k = uv.Key{Code: replacement}
						goto mapped
					}
				}
			}
		}
		if k.Code == uv.KeyUp || k.Code == uv.KeyDown || k.Code == 'h' || k.Code == 'j' || k.Code == 'k' || k.Code == 'l' {
			return
		}
	}
mapped:
	switch {
	case k.Code == 'h' || k.Code == 'j' || k.Code == 'k' || k.Code == 'l':
		a.prefixKey(k)
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
		order := a.st.VisibleWorkspaceOrder()
		if index < len(order) {
			a.exec(model.CmdSelectWorkspace{Index: order[index]})
			a.workspaceNavigate = false
		}
	default:
		if a.sidebarConfig.Keys.Actions != nil {
			a.configuredKey(k, true)
		} else {
			a.workspaceNavigate = k.Code == 'h' || k.Code == 'j' || k.Code == 'k' || k.Code == 'l' || k.Code == uv.KeyLeft || k.Code == uv.KeyRight
			a.prefixKey(k)
		}
	}
	a.dirty = true
}

func (a *App) resizeExitKey(k uv.Key) bool {
	for _, spec := range a.sidebarConfig.ActionBindings("resize_mode") {
		chord, err := config.ParseChord(spec)
		if err == nil && chord.Match(k, chord.Prefix) {
			return true
		}
	}
	return false
}
