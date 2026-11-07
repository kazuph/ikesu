package tui

import "github.com/kazuph/ikesu/internal/model"

// Hidden machine endpoints still supply sidebar snapshots, but cannot mark
// panes seen or receive clipboard writes until this terminal displays them.
func (v *Viewer) SetVisible(a *App, visible bool) {
	if !visible {
		v.app.windowBlurred = true
		return
	}
	if v.app.windowBlurred {
		// Queries emitted while this machine was hidden did not reach the host.
		v.with(a, func(ui *App) { ui.queryHostMouse() })
	}
	v.Input(a, []byte("\x1b[I"))
}

// SelectWorkspace uses the remote workspace's original ID and changes only
// this viewer's navigation, preserving the session's canonical selection.
func (v *Viewer) SelectWorkspace(a *App, id int) bool {
	for index, workspace := range a.st.Workspaces {
		if workspace.ID == id {
			v.with(a, func(ui *App) { ui.exec(model.CmdSelectWorkspace{Index: index}) })
			return true
		}
	}
	return false
}

func (v *Viewer) HasWorkspace(a *App, id int, generation string) bool {
	for _, ws := range a.st.Workspaces {
		if ws.ID == id && generation != "" && ws.Generation == generation {
			return true
		}
	}
	return false
}

func (a *App) isOyagoiWorkspace(ws *model.Workspace) bool {
	return a.oyagoiWorkspace != 0 && ws.ID == a.oyagoiWorkspace && a.oyagoiGeneration != "" && ws.Generation == a.oyagoiGeneration
}

func (v *Viewer) OpenWorkspaceMenu(a *App, id int, generation string, x, y int) bool {
	for index, ws := range a.st.Workspaces {
		if ws.ID == id && generation != "" && ws.Generation == generation {
			v.with(a, func(ui *App) { ui.openWorkspaceMenu(x, y, index); ui.dirty = true })
			return true
		}
	}
	return false
}

func (v *Viewer) SetOyagoi(a *App, id int, generation string) {
	chosen := 0
	for _, ws := range a.st.Workspaces {
		if id != 0 && ws.ID == id && generation != "" && ws.Generation == generation {
			chosen = id
			break
		}
	}
	v.with(a, func(ui *App) { ui.oyagoiWorkspace = chosen; ui.oyagoiGeneration = generation; ui.dirty = true })
}

func (v *Viewer) SetForeignOyagoi(a *App, label string, online bool) {
	v.with(a, func(ui *App) { ui.oyagoiForeignName, ui.oyagoiForeignOnline = label, online; ui.dirty = true })
}
