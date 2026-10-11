package tui

// Notification suppression follows each attached viewer's selected tab,
// including unfocused splits and panes hidden by zoom.
func (v *Viewer) PaneOnSelectedTab(a *App, pid int) bool {
	for _, ws := range a.st.Workspaces {
		if ws.ID == v.workspace {
			for _, tab := range ws.Tabs {
				if tab.ID == v.tabs[ws.ID] {
					return tab.Layout.HasPane(pid)
				}
			}
		}
	}
	return false
}

// DisplaysPane includes unfocused splits, but excludes panes hidden by
// zoom or another selected tab. Clipboard routing does not alter focus.
func (v *Viewer) DisplaysPane(a *App, pid int) bool {
	if v.app.windowBlurred {
		return false
	}
	for _, ws := range a.st.Workspaces {
		if ws.ID != v.workspace {
			continue
		}
		if popup := a.st.Popup; popup != nil && popup.WorkspaceID == ws.ID && popup.PaneID == pid {
			return true
		}
		for _, tab := range ws.Tabs {
			if tab.ID == v.tabs[ws.ID] {
				if overlay := a.st.Overlay; overlay != nil && overlay.WorkspaceID == ws.ID && overlay.TabID == tab.ID && tab.Layout.HasPane(overlay.TargetPaneID) {
					if overlay.PaneID == pid {
						return true
					}
					if overlay.TargetPaneID == pid {
						return false
					}
				}
				if tab.Zoomed != 0 && tab.Layout.HasPane(tab.Zoomed) {
					return tab.Zoomed == pid
				}
				return tab.Layout.HasPane(pid)
			}
		}
	}
	return false
}
