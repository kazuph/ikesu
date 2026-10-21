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
