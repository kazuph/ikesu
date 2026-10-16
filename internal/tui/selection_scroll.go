package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"time"
)

// Observed edge movement is one row per 33 ms; outside movement adds two
// rows per cell beyond the viewport (stage7 k28-timing captures).
const selectionScrollInterval = 33 * time.Millisecond

func (a *App) selectionDragScroll(m uv.Mouse) {
	s := a.sel
	if s == nil || !s.moved {
		return
	}
	dir, distance := 0, 0
	switch {
	case m.Y <= s.inner.Y:
		dir, distance = 1, s.inner.Y-m.Y
	case m.Y >= s.inner.Y+s.inner.H-1:
		dir, distance = -1, m.Y-(s.inner.Y+s.inner.H-1)
	}
	if dir != s.autoDir {
		s.autoAt = time.Now()
	}
	s.autoDir = dir
	if distance > 0 {
		a.selectionScrollBy(2 * distance * dir)
	}
}

func (a *App) selectionAutoScroll(now time.Time) {
	s := a.sel
	if s == nil || s.autoDir == 0 || !s.moved {
		return
	}
	steps := int(now.Sub(s.autoAt) / selectionScrollInterval)
	if steps <= 0 {
		return
	}
	s.autoAt = s.autoAt.Add(time.Duration(steps) * selectionScrollInterval)
	a.selectionScrollBy(steps * s.autoDir)
}

func (a *App) selectionScrollBy(delta int) {
	s := a.sel
	p := a.sup.Get(s.pane)
	if p == nil {
		return
	}
	a.setScrollOff(s.pane, min(max(a.scroll[s.pane]+delta, 0), p.Snapshot().ScrollbackLen))
	s.hy = viewTop(p, a.scroll[s.pane], s.inner.H)
	if s.autoDir < 0 {
		s.hy += s.inner.H - 1
	}
	a.dirty = true
}
