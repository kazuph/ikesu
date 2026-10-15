package tui

import (
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

type tabGesture struct {
	ws, tab, x, y, drop int
	moved               bool
}

func (a *App) tabGestureMouse(ev input.Event) {
	g := a.tabGesture
	if g == nil {
		return
	}
	m := ev.Mouse
	if ev.Kind == input.KindMouseMotion {
		if m.X != g.x || m.Y != g.y {
			g.moved = true
		}
		g.drop = -1
		if g.ws == a.st.Active {
			g.drop = render.TabDropIndex(a.tabHits, m.X, m.Y)
		}
	}
	if ev.Kind == input.KindMouseRelease {
		a.tabGesture = nil
		if g.ws != a.st.Active {
			return
		}
		if g.moved {
			g.drop = render.TabDropIndex(a.tabHits, m.X, m.Y)
			if g.drop >= 0 {
				a.exec(model.CmdMoveTab{Workspace: g.ws, Index: g.tab, InsertIndex: g.drop})
			}
			return
		}
		if g.tab < len(a.tabHits) && pointIn(m.X, m.Y, a.tabHits[g.tab]) {
			a.exec(model.CmdSelectTab{Workspace: g.ws, Tab: g.tab})
		}
	}
}
