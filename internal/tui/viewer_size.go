package tui

import (
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
)

// A zoomed pane uses the whole area; its hidden siblings still receive their
// normal split rectangles at resize, rather than catching up on unzoom.
func (a *App) ptyPaneRects(tab *model.Tab, area layout.Rect) []layout.PaneRect {
	base := *tab
	base.Zoomed = 0
	rects := a.st.PaneRectsForDisplay(&base, area)
	for i := range rects {
		if rects[i].ID == tab.Zoomed {
			rects[i].Rect = area
		}
	}
	return rects
}

// withPTYGeometry keeps each viewer's render viewport independent of the
// attachment which determines the shared terminals' dimensions.
type ptySizeBasis struct {
	viewer                   *Viewer
	cols, rows, cellW, cellH int
	display                  displayState
}

func (v *Viewer) sizeBasis() ptySizeBasis {
	return ptySizeBasis{v, v.cols, v.rows, v.app.cellW, v.app.cellH, v.display}
}

func (a *App) withPTYGeometry(fn func()) {
	a.withTabPTYGeometry(a.currentSizeTab(), fn)
}

func (a *App) withTabPTYGeometry(tab *model.Tab, fn func()) {
	root := a
	if a.viewerOwner != nil {
		root = a.viewerOwner
	}
	basis, ok := root.sizeOwners[tab]
	if !ok && root.sizeViewer != nil {
		basis = root.sizeViewer.sizeBasis()
		ok = true
	}
	if !ok {
		fn()
		return
	}
	cols, rows := a.st.Cols, a.st.Rows
	display := captureDisplay(a.st)
	cw, ch := a.cellW, a.cellH
	a.st.Cols, a.st.Rows = basis.cols, basis.rows
	if a.viewerContext != basis.viewer {
		basis.display.apply(a.st)
	} else {
		basis.display = display
		root.sizeOwners[tab] = basis
	}
	a.cellW, a.cellH = basis.cellW, basis.cellH
	defer func() { a.st.Cols, a.st.Rows = cols, rows; display.apply(a.st); a.cellW, a.cellH = cw, ch }()
	fn()
}

func (a *App) ptyRectForPane(pid int) (r layout.Rect) {
	tab := a.currentSizeTab()
	for _, ws := range a.st.Workspaces {
		for _, candidate := range ws.Tabs {
			if candidate.Layout.HasPane(pid) {
				tab = candidate
			}
		}
	}
	a.withTabPTYGeometry(tab, func() { r = a.rectForPane(pid) })
	return r
}

func (v *Viewer) takeSize(a *App) {
	found := false
	for _, attached := range a.sizeViewers {
		if attached == v {
			found = true
			break
		}
	}
	if !found {
		a.sizeViewers = append(a.sizeViewers, v)
	}
	a.sizeViewer = v
	if a.sizeOwners == nil {
		a.sizeOwners = make(map[*model.Tab]ptySizeBasis)
	}
	basis := v.sizeBasis()
	basis.display = captureDisplay(a.st)
	a.sizeOwners[a.currentSizeTab()] = basis
}

// Detach replaces a departing size owner with the first remaining display
// of that tab. A non-owner disconnect does not steal the current ownership.
func (v *Viewer) Detach(a *App) {
	for i, attached := range a.sizeViewers {
		if attached != v {
			continue
		}
		a.sizeViewers = append(a.sizeViewers[:i], a.sizeViewers[i+1:]...)
		for tab, basis := range a.sizeOwners {
			if basis.viewer == v {
				v.releaseSizeTab(a, tab)
			}
		}
		if len(a.sizeViewers) == 0 {
			a.sizeOwners = nil
			a.sizeViewer = nil
		}
		if len(a.sizeViewers) == 1 {
			remaining := a.sizeViewers[0]
			remaining.with(a, func(ui *App) { remaining.takeSize(a); ui.syncLayout() })
			a.st.Cols, a.st.Rows = remaining.cols, remaining.rows
		}
		return
	}
}

func (v *Viewer) sizeTab(a *App) *model.Tab {
	for _, ws := range a.st.Workspaces {
		if ws.ID != v.workspace {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab.ID == v.tabs[ws.ID] {
				return tab
			}
		}
	}
	return nil
}

func (v *Viewer) releaseSizeTab(a *App, tab *model.Tab) {
	if a.sizeOwners[tab].viewer != v {
		return
	}
	for _, remaining := range a.sizeViewers {
		if remaining != v && remaining.sizeTab(a) == tab {
			remaining.with(a, func(ui *App) { remaining.takeSize(a); ui.syncLayout() })
			return
		}
	}
}

func (a *App) currentSizeTab() *model.Tab {
	if ws := a.st.ActiveWorkspace(); ws != nil {
		return ws.TabRef()
	}
	return nil
}

func (a *App) forgetClosedSizeTabs() {
	for tab := range a.sizeOwners {
		present := false
		for _, ws := range a.st.Workspaces {
			for _, live := range ws.Tabs {
				if live == tab {
					present = true
				}
			}
		}
		if !present {
			delete(a.sizeOwners, tab)
		}
	}
}
