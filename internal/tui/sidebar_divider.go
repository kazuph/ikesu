package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"time"
)

// Preserve the existing divider double-click window while deferring singles.
const sidebarDividerDoubleClick = 350 * time.Millisecond

type sidebarDividerClick struct{ at time.Time }

func (a *App) beginSidebarDivider(m uv.Mouse, right bool, now time.Time) {
	a.flushSidebarDividerClicks(now)
	side := 0
	if right {
		side = 1
	}
	pending := a.dividerClicks[side]
	double := pending != nil && now.Sub(pending.at) <= sidebarDividerDoubleClick
	a.dividerClicks[side] = nil
	a.sidebarGesture = &sidebarGesture{divider: true, right: right, double: double, x: m.X, y: m.Y}
}
func (a *App) finishSidebarDivider(g *sidebarGesture, now time.Time) {
	side := 0
	if g.right {
		side = 1
	}
	if g.moved {
		a.dividerClicks[side] = nil
		a.saveViewPreferences()
		return
	}
	if !g.double {
		a.dividerClicks[side] = &sidebarDividerClick{at: now}
		return
	}
	if g.right {
		a.st.RightSidebarMode = ""
		a.st.RightSidebarManual = false
		_, _, normal := a.st.SidebarBounds()
		a.st.RightSidebarSize = normal
		if width := a.sidebarConfig.UI.RightSidebarWidth; width != nil {
			lo, hi, _ := a.st.SidebarBounds()
			a.st.RightSidebarSize = max(lo, min(hi, *width))
		}

	} else {
		a.st.SidebarVisible = true
		a.st.SidebarHidden = false
		a.st.SetSidebarPreset("normal")
	}
	a.syncLayout()
	a.saveViewPreferences()
	a.changed()
}
func (a *App) flushSidebarDividerClicks(now time.Time) {
	for side, pending := range a.dividerClicks {
		if pending == nil || now.Sub(pending.at) <= sidebarDividerDoubleClick {
			continue
		}
		a.dividerClicks[side] = nil
		if side == 1 {
			if a.st.RightSidebarMode == "" {
				a.st.RightSidebarMode = "compact"
			} else {
				a.st.RightSidebarMode = ""
				a.st.RightSidebarSize = a.st.ExpandedRightSidebarWidth()
			}
		} else {
			a.st.SidebarVisible = !a.st.SidebarVisible
			if a.st.SidebarVisible {
				a.st.SidebarSize = a.st.ExpandedSidebarWidth()
			}
			a.st.SidebarHidden = false
		}
		a.syncLayout()
		a.saveViewPreferences()
		a.changed()
	}
}
