package tui

import (
	"encoding/base64"
	"fmt"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

// onMouse is the mouse entry point; overlays grab events before panes.
func (a *App) onMouse(ev input.Event) {
	m := ev.Mouse
	switch {
	case a.dialog != nil:
		a.dialogMouse(ev)
	case a.menu != nil:
		a.menuMouse(ev)
	case a.sep != nil:
		a.sepMouse(ev)
	case a.sel != nil:
		a.selMouse(ev)
	case a.fwd >= 0:
		a.forwardMouse(ev)
	default:
		switch ev.Kind {
		case input.KindMouseClick:
			a.click(m)
		case input.KindMouseWheel:
			a.wheel(m)
		}
	}
	a.dirty = true
}

// click dispatches a mouse press by region.
func (a *App) click(m uv.Mouse) {
	if m.Y == a.st.StatusArea().Y {
		a.statusClick(m)
		return
	}
	if pointIn(m.X, m.Y, a.st.SidebarArea()) {
		a.sidebarClick(m)
		return
	}
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return
	}
	area := a.st.PanesArea()

	if m.Button == uv.MouseRight {
		if pr, ok := a.paneAt(m.X, m.Y); ok {
			p := a.sup.Get(pr.ID)
			tracking := p != nil && p.Snapshot().MouseTracking
			// The pane border is always ours: right-click there opens the
			// menu even while the app tracks the mouse.
			if tracking && !isBorderCell(pr.Rect, m.X, m.Y) {
				a.exec(model.CmdFocusPane{Pane: pr.ID})
				a.forwardTo(pr.ID, uv.MouseClickEvent(m))
				a.fwd = pr.ID
				return
			}
			a.exec(model.CmdFocusPane{Pane: pr.ID})
			a.openPaneMenu(m.X, m.Y, pr.ID)
		}
		return
	}

	if m.Button != uv.MouseLeft {
		return
	}
	// Split-border drag beats pane focus/selection.
	if b := ws.Layout.SplitBorderAt(m.X, m.Y, area); b != nil {
		a.sep = &sepDrag{path: b.Path, dir: b.Direction, area: b.Area, l: ws.Layout}
		return
	}
	pr, ok := a.paneAt(m.X, m.Y)
	if !ok {
		return
	}
	a.exec(model.CmdFocusPane{Pane: pr.ID})
	if isBorderCell(pr.Rect, m.X, m.Y) {
		return
	}
	p := a.sup.Get(pr.ID)
	if p == nil {
		return
	}
	inner := layout.Rect{X: pr.Rect.X + 1, Y: pr.Rect.Y + 1, W: pr.Rect.W - 2, H: pr.Rect.H - 2}
	if p.Snapshot().MouseTracking {
		a.forwardTo(pr.ID, uv.MouseClickEvent(m))
		a.fwd = pr.ID
		return
	}
	off := a.scroll[pr.ID]
	a.sel = &selState{
		pane: pr.ID,
		ax:   m.X - inner.X, ay: viewTop(p, off, inner.H) + m.Y - inner.Y,
		hx: m.X - inner.X, hy: viewTop(p, off, inner.H) + m.Y - inner.Y,
		inner: inner, scrollAt: off,
	}
}

func (a *App) paneAt(x, y int) (layout.PaneRect, bool) {
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return layout.PaneRect{}, false
	}
	for _, pr := range ws.Layout.Panes(a.st.PanesArea()) {
		if pointIn(x, y, pr.Rect) {
			return pr, true
		}
	}
	return layout.PaneRect{}, false
}

func isBorderCell(r layout.Rect, x, y int) bool {
	return x == r.X || x == r.X+r.W-1 || y == r.Y || y == r.Y+r.H-1
}

// statusClick handles the status-line buttons.
func (a *App) statusClick(m uv.Mouse) {
	if m.Button != uv.MouseLeft || a.st.Cols < 60 {
		return
	}
	sv, sh, cl := render.StatusButtons(a.st.Cols, a.st.Rows)
	switch {
	case pointIn(m.X, m.Y, sv):
		a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	case pointIn(m.X, m.Y, sh):
		a.exec(model.CmdSplitFocused{Dir: layout.Horizontal})
	case pointIn(m.X, m.Y, cl):
		a.closeFocusedPane()
	}
}

// sidebarClick handles clicks inside the sidebar region.
func (a *App) sidebarClick(m uv.Mouse) {
	if m.Button != uv.MouseLeft {
		return
	}
	s := a.st
	area := s.SidebarArea()
	if pointIn(m.X, m.Y, render.SidebarToggleRect(s)) {
		a.exec(model.CmdToggleSidebar{})
		return
	}
	if !s.SidebarVisible {
		// Collapsed strip: one row per workspace.
		ix, iy, iw, ih := area.X+1, area.Y+1, area.W-2, area.H-2
		_ = ix
		_ = iw
		scroll := a.clampCollapsedScroll(len(s.Workspaces), ih)
		i := m.Y - iy + scroll
		if i >= 0 && i < len(s.Workspaces) {
			a.exec(model.CmdSelectWorkspace{Index: i})
		}
		return
	}
	// Footer buttons.
	if pointIn(m.X, m.Y, render.SidebarMenuRect(s)) {
		a.openMainMenu(m.X, m.Y)
		return
	}
	if pointIn(m.X, m.Y, render.SidebarNewRect(s)) {
		a.newWorkspace()
		return
	}
	// Rows.
	_, iy, _, avail := render.SidebarContent(area)
	if m.Y < iy || m.Y >= iy+avail {
		return
	}
	rows := render.SidebarRows(s, a.paneSnaps())
	scroll := a.sidebarScroll
	if max := len(rows) - avail; scroll > max {
		scroll = max
	}
	i := m.Y - iy + scroll
	if i < 0 || i >= len(rows) {
		return
	}
	r := rows[i]
	switch r.Kind {
	case render.RowWorkspace:
		if r.Ws == s.Active {
			a.openWorkspaceMenu(m.X, m.Y, r.Ws)
		} else {
			a.exec(model.CmdSelectWorkspace{Index: r.Ws})
		}
	case render.RowAgent:
		a.exec(model.CmdSelectWorkspace{Index: r.Ws})
		a.exec(model.CmdFocusPane{Pane: r.Pane})
	}
}

func (a *App) clampCollapsedScroll(n, avail int) int {
	scroll := a.sidebarScroll
	if max := n - avail; scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	return scroll
}

// wheel routes scroll events per region (3.6).
func (a *App) wheel(m uv.Mouse) {
	up := m.Button == uv.MouseWheelUp
	if !up && m.Button != uv.MouseWheelDown {
		return
	}
	// Sidebar scrolls its rows.
	if pointIn(m.X, m.Y, a.st.SidebarArea()) {
		if up {
			a.sidebarScroll -= 3
			if a.sidebarScroll < 0 {
				a.sidebarScroll = 0
			}
		} else {
			a.sidebarScroll += 3
		}
		return
	}
	pr, ok := a.paneAt(m.X, m.Y)
	if !ok {
		return
	}
	p := a.sup.Get(pr.ID)
	if p == nil {
		return
	}
	sn := p.Snapshot()
	switch {
	case sn.MouseTracking:
		a.forwardTo(pr.ID, uv.MouseWheelEvent(m))
	case sn.AltScreen:
		// Full-screen apps without mouse support scroll via arrow keys.
		code := uv.KeyUp
		if !up {
			code = uv.KeyDown
		}
		p.WithEmu(func(e *vt.Emulator) {
			for i := 0; i < 3; i++ {
				e.SendKey(uv.KeyPressEvent{Code: code})
			}
		})
	default:
		off := a.scroll[pr.ID]
		if up {
			off += 3
			if off > sn.ScrollbackLen {
				off = sn.ScrollbackLen
			}
			a.scroll[pr.ID] = off
		} else {
			off -= 3
			if off <= 0 {
				delete(a.scroll, pr.ID)
			} else {
				a.scroll[pr.ID] = off
			}
		}
	}
}

// --- drag states ---

// sepMouse updates the ratio while a split border is dragged.
func (a *App) sepMouse(ev input.Event) {
	m := ev.Mouse
	if ev.Kind == input.KindMouseRelease {
		a.sep = nil
		return
	}
	if ev.Kind != input.KindMouseMotion {
		return
	}
	s := a.sep
	var ratio float64
	if s.dir == layout.Vertical {
		ratio = float64(m.X-s.area.X) / float64(max(1, s.area.W))
	} else {
		ratio = float64(m.Y-s.area.Y) / float64(max(1, s.area.H))
	}
	if s.l.SetRatioAt(s.path, ratio) {
		a.syncLayout()
	}
}

// selMouse updates/finishes the drag selection.
func (a *App) selMouse(ev input.Event) {
	m := ev.Mouse
	s := a.sel
	p := a.sup.Get(s.pane)
	if p == nil {
		a.sel = nil
		return
	}
	top := viewTop(p, a.scroll[s.pane], s.inner.H)
	hx := m.X - s.inner.X
	hy := top + m.Y - s.inner.Y
	if hx < 0 {
		hx = 0
	}
	if hx >= s.inner.W {
		hx = s.inner.W - 1
	}
	s.hx, s.hy = hx, hy
	if hx != s.ax || hy != s.ay {
		s.moved = true
	}
	if ev.Kind == input.KindMouseRelease {
		a.finishSelection()
	}
}

// finishSelection copies the selection via OSC 52 and shows the flash.
func (a *App) finishSelection() {
	s := a.sel
	a.sel = nil
	if s == nil || !s.moved {
		return
	}
	text, chars := a.selText()
	if text == "" {
		return
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(text))
	fmt.Fprintf(a.out, "\x1b]52;c;%s\a", b64)
	a.copied = fmt.Sprintf(" copied %d chars ", chars)
	a.copiedAt = time.Now()
}

// forwardMouse keeps relaying events to a mouse-tracking pane during a drag.
func (a *App) forwardMouse(ev input.Event) {
	pid := a.fwd
	m := ev.Mouse
	switch ev.Kind {
	case input.KindMouseMotion:
		a.forwardTo(pid, uv.MouseMotionEvent(m))
	case input.KindMouseRelease:
		a.forwardTo(pid, uv.MouseReleaseEvent(m))
		a.fwd = -1
	case input.KindMouseClick:
		a.forwardTo(pid, uv.MouseClickEvent(m))
	case input.KindMouseWheel:
		a.forwardTo(pid, uv.MouseWheelEvent(m))
	}
}

// forwardTo sends a mouse event to a pane in pane-inner coordinates.
func (a *App) forwardTo(pid int, ev uv.MouseEvent) {
	p := a.sup.Get(pid)
	if p == nil {
		return
	}
	r, ok := a.paneRect(pid)
	if !ok {
		return
	}
	tr := func(m uv.Mouse) uv.Mouse {
		return uv.Mouse{X: m.X - r.X - 1, Y: m.Y - r.Y - 1, Button: m.Button, Mod: m.Mod}
	}
	switch e := ev.(type) {
	case uv.MouseClickEvent:
		p.SendMouse(uv.MouseClickEvent(tr(uv.Mouse(e))))
	case uv.MouseReleaseEvent:
		p.SendMouse(uv.MouseReleaseEvent(tr(uv.Mouse(e))))
	case uv.MouseMotionEvent:
		p.SendMouse(uv.MouseMotionEvent(tr(uv.Mouse(e))))
	case uv.MouseWheelEvent:
		p.SendMouse(uv.MouseWheelEvent(tr(uv.Mouse(e))))
	}
}
