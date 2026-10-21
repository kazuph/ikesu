package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func (a *App) legacyCompactSidebar() bool {
	return a.viewerContext != nil && a.viewerContext.legacySidebarRows && a.st.RightSidebarCompact()
}

func (a *App) legacyRightSidebarClick(m uv.Mouse) {
	area := a.st.RightSidebarArea()
	rows := render.LegacyRightSidebarRows(a.rightSidebarInput())
	offset := max(0, min(a.agentScroll, max(0, len(rows)-area.H)))
	i := m.Y - area.Y + offset
	if i < 0 || i >= len(rows) {
		return
	}
	r := rows[i]
	if r.Kind == render.RowAgent && (m.Button == uv.MouseLeft || m.Button == uv.MouseRight) {
		a.exec(model.CmdSelectWorkspace{Index: r.Ws})
		a.exec(model.CmdFocusPane{Pane: r.Pane})
		if m.Button == uv.MouseRight {
			a.openPaneMenu(m.X, m.Y, r.Pane)
		}
	} else if r.Kind == render.RowJob && m.Button == uv.MouseLeft {
		a.focusJobCaller(r.Line)
	}
}
