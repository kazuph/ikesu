package tui

import (
	"github.com/kazuph/ikesu/internal/model"
	"time"
)

// Interaction state belongs to each viewer, not the PTY/output snapshot.
func (a *App) showPaneScrollbar(pid int) {
	if a.scrollbarUntil == nil {
		a.scrollbarUntil = map[int]time.Time{}
	}
	a.scrollbarUntil[pid] = time.Now().Add(a.sidebarConfig.PaneScrollbarHideDelay())
	a.dirty = true
}

func (a *App) paneScrollbarVisible(pid int, inner model.Rect, scrollback int, now time.Time) bool {
	return model.PaneScrollbarAvailable(inner, scrollback) && a.paneScrollbarActive(pid, now)
}

func (a *App) paneScrollbarActive(pid int, now time.Time) bool {
	switch a.sidebarConfig.PaneScrollbarMode() {
	case "off":
		return false
	case "always":
		return true
	}
	return a.sb != nil && a.sb.pane == pid || now.Before(a.scrollbarUntil[pid])
}

func (a *App) expirePaneScrollbars(now time.Time) {
	for pid, until := range a.scrollbarUntil {
		if !now.Before(until) && (a.sb == nil || a.sb.pane != pid) {
			delete(a.scrollbarUntil, pid)
			a.dirty = true
		}
	}
}
