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
func (a *App) withPTYGeometry(fn func()) {
	root := a
	if a.viewerOwner != nil {
		root = a.viewerOwner
	}
	owner := root.sizeOwners[a.currentSizeTab()]
	if owner == nil {
		owner = root.sizeViewer
	}
	if owner == nil {
		fn()
		return
	}
	cols, rows := a.st.Cols, a.st.Rows
	display := captureDisplay(a.st)
	cw, ch := a.cellW, a.cellH
	a.st.Cols, a.st.Rows = owner.cols, owner.rows
	if a.viewerContext != owner {
		owner.display.apply(a.st)
	}
	a.cellW, a.cellH = owner.app.cellW, owner.app.cellH
	defer func() {
		a.st.Cols, a.st.Rows = cols, rows
		display.apply(a.st)
		a.cellW, a.cellH = cw, ch
	}()
	fn()
}

func (a *App) ptyRectForPane(pid int) (r layout.Rect) {
	a.withPTYGeometry(func() { r = a.rectForPane(pid) })
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
		a.sizeOwners = make(map[*model.Tab]*Viewer)
	}
	a.sizeOwners[a.currentSizeTab()] = v
}

// Detach freezes the current tab sizes while multiple displays remain. The
// final remaining display immediately sizes only the tab it is showing.
func (v *Viewer) Detach(a *App) {
	for i, attached := range a.sizeViewers {
		if attached != v {
			continue
		}
		a.sizeViewers = append(a.sizeViewers[:i], a.sizeViewers[i+1:]...)
		if len(a.sizeViewers) == 1 {
			remaining := a.sizeViewers[0]
			remaining.with(a, func(ui *App) { remaining.takeSize(a); ui.syncLayout() })
			a.st.Cols, a.st.Rows = remaining.cols, remaining.rows
		}
		return
	}
}

func (a *App) currentSizeTab() *model.Tab {
	if ws := a.st.ActiveWorkspace(); ws != nil {
		return ws.TabRef()
	}
	return nil
}
