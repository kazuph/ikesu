package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/model"
	"time"
)

// Hover and horizontal wheel belong to the application body, not the pane
// border, scrollbar gutter, sidebar, or tab strip. Overlays route first.
func (a *App) forwardTrackingBody(m uv.Mouse, event uv.MouseEvent) {
	pr, ok := a.paneAt(m.X, m.Y)
	if !ok {
		return
	}
	inner, gutter := model.PaneContentRect(pr.Rect, a.st.PaneBorders)
	p := a.sup.Get(pr.ID)
	if p == nil {
		return
	}
	snapshot := p.Snapshot()
	if gutter > 0 && a.paneScrollbarVisible(pr.ID, inner, snapshot.ScrollbackLen, time.Now()) {
		inner.W--
	}
	if !pointIn(m.X, m.Y, inner) {
		return
	}
	if snapshot.MouseTracking {
		a.forwardTo(pr.ID, event)
	}
}
