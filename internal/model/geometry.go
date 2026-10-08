package model

import (
	"github.com/kazuph/goterrace/internal/layout"
)

// TabBarVisible reports whether the tab strip is drawn: on unless
// show_tab_bar=false, or a lone tab with hide_tab_bar_when_single_tab.
func (s *AppState) TabBarVisible() bool {
	if !s.ShowTabBar {
		return false
	}
	ws := s.ActiveWorkspace()
	if s.HideTabBarWhenSingleTab && ws != nil && len(ws.Tabs) <= 1 {
		return false
	}
	return true
}

// TabBarArea returns the one-row tab strip above the panes, or an empty
// rect when the bar is hidden.
func (s *AppState) TabBarArea() Rect {
	w := SidebarCollapsedWidth
	if s.SidebarVisible {
		w = s.ExpandedSidebarWidth()
	}
	if !s.TabBarVisible() {
		return Rect{}
	}
	return Rect{X: w, Y: 0, W: max0(s.Cols - w), H: 1}
}

// ActionBarArea is the one-row bottom pane action bar (SPEC G1): the
// main area's last row, right of the sidebar and above the status line.
// It shows only on desktop with an active workspace and at least two
// rows left for the panes.
func (s *AppState) ActionBarArea() Rect {
	w := SidebarCollapsedWidth
	if s.SidebarVisible {
		w = s.ExpandedSidebarWidth()
	}
	if s.ActiveWorkspace() == nil {
		return Rect{}
	}
	if s.Rows-3 < 2 { // need ≥2 pane rows: bar + status take the rest
		return Rect{}
	}
	return Rect{X: w, Y: max0(s.Rows - 2), W: max0(s.Cols - w), H: 1}
}

// PanesArea returns the rect the pane layout fills: right of the sidebar,
// below the (possibly hidden) tab bar, above the action bar and status.
func (s *AppState) PanesArea() Rect {
	w := SidebarCollapsedWidth
	if s.SidebarVisible {
		w = s.ExpandedSidebarWidth()
	}
	y := 0
	if s.TabBarVisible() {
		y = 1
	}
	bottom := s.Rows - 1 // status line
	if ab := s.ActionBarArea(); ab.H > 0 {
		bottom = ab.Y
	}
	return Rect{X: w, Y: y, W: max0(s.Cols - w), H: max0(bottom - y)}
}

// SidebarArea returns the sidebar rect (may be the collapsed strip).
func (s *AppState) SidebarArea() Rect {
	w := SidebarCollapsedWidth
	if s.SidebarVisible {
		w = s.ExpandedSidebarWidth()
	}
	return Rect{X: 0, Y: 0, W: min(s.Cols, w), H: max0(s.Rows - 1)}
}

// StatusArea returns the one-line status bar rect.
func (s *AppState) StatusArea() Rect {
	return Rect{X: 0, Y: max0(s.Rows - 1), W: s.Cols, H: 1}
}

const (
	// SidebarWidth is the expanded sidebar width in cells.
	SidebarWidth = SidebarNormalDefault
	// SidebarCollapsedWidth is the collapsed strip width in cells.
	SidebarCollapsedWidth = 3
)

// Rect re-exports the layout rect for convenience.
type Rect = layout.Rect

// PaneContentRect resolves a pane rect to its terminal-content rect and
// gutter column count per SPEC G1: a frame eats one cell on each side but
// only when borders are on and the pane is wider and taller than 2; the
// rightmost inner column is a scrollbar gutter whenever the framed inner
// width is at least 5.
func PaneContentRect(r Rect, borders bool) (inner Rect, gutter int) {
	if !borders || r.W <= 2 || r.H <= 2 {
		return r, 0
	}
	inner = Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}
	if inner.W >= 5 {
		gutter = 1
	}
	return inner, gutter
}

// PaneTermSize is the PTY cell size inside a pane rect: the content rect
// minus its gutter column.
func PaneTermSize(r Rect, borders bool) (w, h int) {
	inner, gutter := PaneContentRect(r, borders)
	return max(1, inner.W-gutter), max(1, inner.H)
}
