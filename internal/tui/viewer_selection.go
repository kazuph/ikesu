package tui

import "github.com/kazuph/ikesu/internal/endpoint"

// RestoreSelection restores only surviving IDs. A missing workspace/tab uses
// the first workspace/tab; a missing pane uses that tab's first layout leaf.
func (v *Viewer) RestoreSelection(a *App, selection *endpoint.ViewSelection) {
	if selection == nil {
		return
	}
	v.workspace = selection.Workspace
	v.tabs = make(map[int]int, len(selection.Tabs))
	for workspace, tab := range selection.Tabs {
		v.tabs[workspace] = tab
	}
	v.with(a, func(ui *App) {
		ws := ui.st.ActiveWorkspace()
		if ws == nil {
			return
		}
		tab := ws.TabRef()
		if tab == nil {
			return
		}
		pane := selection.Pane
		if ws.ID != selection.Workspace || tab.ID != selection.Tabs[ws.ID] || !tab.Layout.HasPane(pane) {
			ids := tab.Layout.PaneIDs()
			if len(ids) == 0 {
				return
			}
			pane = ids[0]
		}
		tab.Layout.Focus(pane)
		ui.dirty = true
	})
}
