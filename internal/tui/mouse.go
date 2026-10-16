package tui

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/config"
	"strings"
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
	if a.sidebarConfig.UI.MouseCapture != nil && !*a.sidebarConfig.UI.MouseCapture {
		if pr, ok := a.paneAt(ev.Mouse.X, ev.Mouse.Y); ok {
			if p := a.sup.Get(pr.ID); p != nil && p.Snapshot().MouseTracking {
				switch ev.Kind {
				case input.KindMouseClick:
					a.forwardTo(pr.ID, uv.MouseClickEvent(ev.Mouse))
				case input.KindMouseRelease:
					a.forwardTo(pr.ID, uv.MouseReleaseEvent(ev.Mouse))
				case input.KindMouseMotion:
					a.forwardTo(pr.ID, uv.MouseMotionEvent(ev.Mouse))
				case input.KindMouseWheel:
					a.forwardTo(pr.ID, uv.MouseWheelEvent(ev.Mouse))
				}
			}
		}
		return
	}

	if a.navigator != nil {
		a.navigatorMouse(ev)
		return
	}
	m := ev.Mouse
	if a.settings == nil && a.help == nil && a.dialog == nil && a.menu == nil && a.mobileMouse(ev) {
		a.dirty = true
		return
	}
	if a.settings == nil && a.help == nil && a.dialog == nil && a.menu == nil && a.cm == nil && a.popupMouse(ev) {
		a.dirty = true
		return
	}
	switch {
	case a.settings != nil:
		a.settingsMouse(ev)
	case a.help != nil:
		a.helpMouse(ev)
	case a.dialog != nil:
		a.dialogMouse(ev)
	case a.menu != nil:
		a.menuMouse(ev)
	case a.cm != nil:
		// The action bar keeps its routing precedence inside copy mode
		// (SPEC G1: action bar → border → scrollbar → title → body).
		if ev.Kind == input.KindMouseClick && a.actionBarHit(m) {
			break
		}
		a.copyMouse(ev)
	case a.sidebarBar != nil:
		a.sidebarScrollbarMouse(ev)
	case a.tabGesture != nil:
		a.tabGestureMouse(ev)
	case a.sidebarGesture != nil:
		a.sidebarGestureMouse(ev)
	case a.sep != nil:
		a.sepMouse(ev)
	case a.sb != nil:
		a.sbMouse(ev)
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
	if a.toastClick(m.X, m.Y) {
		return
	}
	if r := a.st.StatusArea(); r.H > 0 && m.Y == r.Y {
		a.statusClick(m)
		return
	}
	if pointIn(m.X, m.Y, a.st.TabBarArea()) {
		a.tabBarClick(m)
		return
	}
	if area := a.st.PanesArea(); a.st.SidebarVisible && m.X == area.X && m.Y >= area.Y && m.Y < area.Y+area.H && m.Button == uv.MouseLeft {
		a.sidebarPress(m)
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
			framed := a.st.PaneBorders && pr.Rect.W > 2 && pr.Rect.H > 2
			// The pane border is always ours: right-click there opens the
			// menu even while the app tracks the mouse. Without borders
			// every cell belongs to the terminal.
			if tracking && a.rightClickPass(m) && !(framed && isBorderCell(pr.Rect, m.X, m.Y)) {
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
	// SPEC G1 left-down routing inside the terminal area, first match
	// fires alone: action bar → split border → pane scrollbar →
	// pane title → pane body.
	if a.actionBarHit(m) {
		return
	}
	if l := ws.ActiveLayout(); l != nil {
		if b := l.SplitBorderAt(m.X, m.Y, area); b != nil {
			grab := b.Pos - m.X
			if b.Direction == layout.Horizontal {
				grab = b.Pos - m.Y
			}
			a.sep = &sepDrag{grab: grab, path: b.Path, dir: b.Direction, area: b.Area, l: l}
			return
		}
	}
	pr, ok := a.paneAt(m.X, m.Y)
	if !ok {
		return
	}
	inner, gutter := model.PaneContentRect(pr.Rect, a.st.PaneBorders)
	framed := a.st.PaneBorders && pr.Rect.W > 2 && pr.Rect.H > 2

	// Pane scrollbar: a press on the thumb starts a drag that keeps the
	// grab offset; a press on the track jumps the thumb to the press
	// point and keeps dragging from there (SPEC G6).
	if gutter > 0 && m.X == inner.X+inner.W-1 &&
		m.Y >= inner.Y && m.Y < inner.Y+inner.H {
		a.exec(model.CmdFocusPane{Pane: pr.ID})
		grab := 0
		if p := a.sup.Get(pr.ID); p != nil {
			sn := p.Snapshot()
			rel := m.Y - inner.Y
			th, top := thumbGeom(inner.H, sn.ScrollbackLen, a.scroll[pr.ID])
			if rel >= top && rel < top+th {
				grab = rel - top
			} else {
				newTop := rel - th/2
				a.setScrollOff(pr.ID, offFromThumb(inner.H, sn.ScrollbackLen, newTop))
				grab = th / 2
			}
		}
		a.sb = &sbDrag{pane: pr.ID, inner: inner, grab: grab}
		return
	}

	// Pane title: left-click focuses the pane and toggles zoom. It
	// never forwards the press to the terminal app.
	if framed && m.Y == pr.Rect.Y {
		a.exec(model.CmdFocusPane{Pane: pr.ID})
		a.exec(model.CmdZoomPane{Pane: pr.ID, Mode: "toggle"})
		a.syncLayout()
		return
	}

	a.exec(model.CmdFocusPane{Pane: pr.ID})
	if framed && isBorderCell(pr.Rect, m.X, m.Y) {
		return
	}
	p := a.sup.Get(pr.ID)
	if p == nil {
		return
	}
	inner.W = max(inner.W-gutter, 1)
	if a.pluginLinkClick(pr.ID, m) {
		return
	}
	if p.Snapshot().MouseTracking {
		// Mouse-reporting panes receive all four events; no menu gesture.
		a.dblMenu = false
		a.forwardTo(pr.ID, uv.MouseClickEvent(m))
		a.fwd = pr.ID
		return
	}
	if a.sidebarConfig.UI.CopyOnSelect != nil && !*a.sidebarConfig.UI.CopyOnSelect {
		return
	}
	off := a.scroll[pr.ID]
	a.sel = &selState{
		pane: pr.ID,
		ax:   m.X - inner.X, ay: viewTop(p, off, inner.H) + m.Y - inner.Y,
		hx: m.X - inner.X, hy: viewTop(p, off, inner.H) + m.Y - inner.Y,
		inner: inner, scrollAt: off,
	}
	// Two same-cell left clicks within 500ms (down+up completed) open the
	// pane menu on the second release (SPEC G1 pane menu). A click that
	// drags into a selection never opens the menu.
	now := time.Now()
	a.dblMenu = a.lastClick.matches(pr.ID, m, now)
	a.lastClick = clickMark{pane: pr.ID, x: m.X, y: m.Y, at: now}
}

// clickMark remembers a left press inside a pane body for the
// double-click-to-menu gesture.
type clickMark struct {
	pane, x, y int
	at         time.Time
	completed  bool
}

func (c clickMark) matches(pane int, m uv.Mouse, now time.Time) bool {
	d := now.Sub(c.at)
	return c.completed && c.pane == pane && c.x == m.X && c.y == m.Y && d >= 0 && d <= 500*time.Millisecond
}

// sbDrag is an active scrollbar-gutter drag on one pane.
type sbDrag struct {
	pane  int
	inner layout.Rect
	grab  int // rows below the thumb's top edge where the drag began
}

// thumbGeom mirrors render.drawGutter: given the inner height, scrollback
// length and scroll offset it returns the thumb's height and its top row
// (relative to the inner rect).
func thumbGeom(h, scrollN, off int) (th, top int) {
	total := scrollN + h
	th = h * h / total
	if th < 1 {
		th = 1
	}
	if th > h {
		th = h
	}
	track := h - th
	top = track
	if scrollN > 0 && track > 0 {
		top = track - int(float64(off)/float64(scrollN)*float64(track))
	}
	return th, top
}

// offFromThumb maps a thumb-top row (relative to inner) back to a
// scrollback offset, clamped to [0, scrollN].
func offFromThumb(h, scrollN, top int) int {
	th, _ := thumbGeom(h, scrollN, 0)
	track := h - th
	if track <= 0 || scrollN <= 0 {
		return 0
	}
	off := int(float64(scrollN)*float64(track-top)/float64(track) + 0.5)
	if off < 0 {
		off = 0
	}
	if off > scrollN {
		off = scrollN
	}
	return off
}

// setScrollOff records a host scrollback offset (0 means live view).
func (a *App) setScrollOff(pid, off int) {
	if off <= 0 {
		delete(a.scroll, pid)
	} else {
		a.scroll[pid] = off
	}
}

// sbMouse maps the drag's y onto the pane's scrollback range while
// keeping the initial grab offset inside the thumb (SPEC G6).
func (a *App) sbMouse(ev input.Event) {
	m := ev.Mouse
	if ev.Kind == input.KindMouseRelease {
		a.sb = nil
		return
	}
	if ev.Kind != input.KindMouseMotion && ev.Kind != input.KindMouseClick {
		return
	}
	p := a.sup.Get(a.sb.pane)
	if p == nil {
		a.sb = nil
		return
	}
	sn := p.Snapshot()
	if sn.ScrollbackLen <= 0 || a.sb.inner.H <= 1 {
		return
	}
	a.setScrollOff(a.sb.pane,
		offFromThumb(a.sb.inner.H, sn.ScrollbackLen, m.Y-a.sb.inner.Y-a.sb.grab))
}

// actionBarHit fires the first matching action-bar button per SPEC G1:
// copy → cycle → rotate → equalize on overlapping cells.
func (a *App) actionBarHit(m uv.Mouse) bool {
	if a.actionBar.Area.W == 0 || m.Y != a.actionBar.Area.Y {
		return false
	}
	if pointIn(m.X, m.Y, a.actionBar.Copy) {
		a.copyCycle()
		return true
	}
	focused := a.st.FocusedPane()
	if pointIn(m.X, m.Y, a.actionBar.Cycle) {
		a.exec(model.CmdArrange{Pane: focused, Op: "cycle"})
		return true
	}
	if pointIn(m.X, m.Y, a.actionBar.Rotate) {
		a.exec(model.CmdArrange{Pane: focused, Op: "rotate_forward"})
		return true
	}
	if pointIn(m.X, m.Y, a.actionBar.Equalize) {
		a.exec(model.CmdArrange{Pane: focused, Op: "equalize"})
		return true
	}
	return pointIn(m.X, m.Y, a.actionBar.Area)
}

// tabBarClick handles selection, scrolling, tab menus and the new-tab prompt.
func (a *App) tabBarClick(m uv.Mouse) {
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return
	}
	if m.Button == uv.MouseLeft {
		if pointIn(m.X, m.Y, a.tabLeftRect) {
			if a.serverUI && m.X == a.tabLeftRect.X {
				return
			}
			a.tabScroll = a.tabScrollLeft
			a.dirty = true
			return
		}
		if pointIn(m.X, m.Y, a.tabRightRect) {
			a.tabScroll = a.tabScrollRight
			a.dirty = true
			return
		}
	}
	if m.Button == uv.MouseLeft && pointIn(m.X, m.Y, a.newTabRect) {
		a.newTabNameDialog()
		return
	}
	for i, r := range a.tabHits {
		if i >= len(ws.Tabs) || !pointIn(m.X, m.Y, r) {
			continue
		}
		switch m.Button {
		case uv.MouseLeft:
			a.tabGesture = &tabGesture{ws: a.st.Active, tab: i, x: m.X, y: m.Y, drop: -1, target: -1}
		case uv.MouseRight:
			a.openTabMenu(m.X, m.Y, a.st.Active, i)
		}
		return
	}
}

// closeTabAt closes workspace ws's tab ti, asking for confirmation when it
// is the workspace's last tab.
func (a *App) closeTabAt(wsIdx, ti int) {
	if wsIdx < 0 || wsIdx >= len(a.st.Workspaces) {
		return
	}
	if len(a.st.Workspaces[wsIdx].Tabs) <= 1 {
		if len(a.st.Workspaces) <= 1 {
			a.replaceLastWorkspace()
		} else {
			a.openCloseWorkspaceDialog(wsIdx)
		}
		return
	}
	a.exec(model.CmdCloseTab{Workspace: wsIdx, Tab: ti})
}

func (a *App) paneAt(x, y int) (layout.PaneRect, bool) {
	if a.st.PopupVisible() {
		r := a.st.PopupRect()
		return layout.PaneRect{ID: a.st.Popup.PaneID, Rect: r}, pointIn(x, y, r)
	}
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return layout.PaneRect{}, false
	}
	t := ws.TabRef()
	if t == nil {
		return layout.PaneRect{}, false
	}
	for _, pr := range a.st.DisplayPaneRects() {
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
	if m.Button == uv.MouseRight {
		a.sidebarRightClick(m)
		return
	}
	if m.Button != uv.MouseLeft {
		return
	}
	if a.sidebarPress(m) {
		return
	}
	s := a.st
	if !a.serverUI && pointIn(m.X, m.Y, render.SidebarToggleRect(s)) {
		a.exec(model.CmdToggleSidebar{})
		return
	}
	if !s.SidebarVisible {
		in := render.Input{State: s, ServerUI: a.serverUI, Panes: a.paneSnaps(), SidebarConfig: a.sidebarConfig, SidebarScroll: a.sidebarScroll, AgentScroll: a.agentScroll}
		if a.serverUI {
			for _, pos := range render.CollapsedAgentRows(in) {
				if pos.Y == m.Y && pos.Row.Kind == render.RowAgent {
					a.exec(model.CmdSelectWorkspace{Index: pos.Row.Ws})
					a.exec(model.CmdFocusPane{Pane: pos.Row.Pane})
					return
				}
			}
		}
		// Collapsed strip: one row per workspace.
		viewport := render.CollapsedWorkspaceViewport(in)
		if !pointIn(m.X, m.Y, viewport.Rect) {
			return
		}
		i := m.Y - viewport.Rect.Y + viewport.Offset
		if i >= 0 && i < len(s.Workspaces) {
			a.exec(model.CmdSelectWorkspace{Index: i})
		}
		return
	}
	// Footer buttons.
	if pointIn(m.X, m.Y, render.SidebarMenuRect(s)) {
		a.openMainMenu(0, render.SidebarMenuRect(s).Y)
		_, h := render.MenuSize(&a.menu.m)
		a.menu.m.Y -= h
		return
	}
	if pointIn(m.X, m.Y, render.SidebarNewRect(s)) {
		a.newWorkspace()
		return
	}
	r, ok := a.sidebarRowAt(m.X, m.Y)
	if !ok {
		return
	}

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

// Card padding retains its workspace owner; empty panel rows stay inert.
func (a *App) sidebarRightClick(m uv.Mouse) {
	s := a.st
	if !s.SidebarVisible {
		return
	}
	if pointIn(m.X, m.Y, render.SidebarMenuRect(s)) || pointIn(m.X, m.Y, render.SidebarNewRect(s)) {
		return
	}
	r, ok := a.sidebarRowAt(m.X, m.Y)
	if ok && (r.Kind == render.RowWorkspace || r.Kind == render.RowGit || r.Kind == render.RowCardBlank) {
		a.openWorkspaceMenu(m.X, m.Y, r.Ws)
	}
}

// wheel routes scroll events per region (3.6).
func (a *App) wheel(m uv.Mouse) {
	up := m.Button == uv.MouseWheelUp
	if !up && m.Button != uv.MouseWheelDown {
		return
	}
	// The tab strip turns the wheel into tab cycling (SPEC G6).
	if pointIn(m.X, m.Y, a.st.TabBarArea()) {
		if a.serverUI {
			return
		}
		if up {
			a.cycleTab(-1)
		} else {
			a.cycleTab(1)
		}
		return
	}
	if pointIn(m.X, m.Y, a.st.SidebarArea()) {
		a.sidebarWheel(m, up)
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
	// The scrollbar gutter is chrome: wheel there always scrolls the
	// host scrollback, never the pane (SPEC G6).
	inner, gutter := model.PaneContentRect(pr.Rect, a.st.PaneBorders)
	if gutter > 0 && m.X == inner.X+inner.W-1 && m.Y >= inner.Y && m.Y < inner.Y+inner.H {
		a.hostScroll(pr.ID, sn.ScrollbackLen, up)
		return
	}
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
			for i := 0; i < a.sidebarConfig.MouseScrollLines(); i++ {
				e.SendKey(uv.KeyPressEvent{Code: code})
			}
		})
	default:
		a.hostScroll(pr.ID, sn.ScrollbackLen, up)
	}
}

// hostScroll moves a pane's host scrollback by three lines per notch.
func (a *App) hostScroll(pid, scrollN int, up bool) {
	off := a.scroll[pid]
	if up {
		off += a.sidebarConfig.MouseScrollLines()
		if off > scrollN {
			off = scrollN
		}
		a.scroll[pid] = off
	} else {
		off -= a.sidebarConfig.MouseScrollLines()
		if off <= 0 {
			delete(a.scroll, pid)
		} else {
			a.scroll[pid] = off
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
		ratio = float64(m.X+s.grab-s.area.X) / float64(max(1, s.area.W))
	} else {
		ratio = float64(m.Y+s.grab-s.area.Y) / float64(max(1, s.area.H))
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
	if !a.st.PopupVisible() && !a.st.OverlayVisible() {
		hy = top + min(max(m.Y-s.inner.Y, 0), s.inner.H-1)
	}
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
	if ev.Kind == input.KindMouseMotion && !a.st.PopupVisible() && !a.st.OverlayVisible() {
		a.selectionDragScroll(m)
	}
	if ev.Kind == input.KindMouseRelease {
		pending := a.dblMenu
		a.dblMenu = false
		a.lastClick.completed = !s.moved && m.X == a.lastClick.x && m.Y == a.lastClick.y
		if pending && !s.moved {
			// Second same-cell down/up completed inside 500ms on a normal
			// pane: open the menu at the click point.
			a.sel = nil
			a.openPaneMenu(m.X, m.Y, s.pane)
			return
		}
		a.finishSelection()
	}
}

// finishSelection copies the selection and shows the flash.
func (a *App) finishSelection() {
	s := a.sel
	a.sel = nil
	if a.sidebarConfig.UI.CopyOnSelect != nil && !*a.sidebarConfig.UI.CopyOnSelect {
		return
	}
	if s == nil || !s.moved {
		return
	}
	text, _ := a.selText(s)
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return
	}
	if err := a.writeClipboard(text); err != nil {
		return
	}
	// SPEC G1: "Copied N lines" on the sidebar's bottom row, 2 seconds.
	lines := strings.Count(text, "\n") + 1
	if strings.HasSuffix(text, "\n") {
		lines--
	}
	if lines < 1 {
		lines = 1
	}
	a.copied = fmt.Sprintf("Copied %d lines", lines)
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
// When the app negotiated SGR 1016 the coordinates are delivered as
// pixels: the center pixel of the target cell, computed from the pane's
// pixel size when known, else the host cell size (SPEC G6).
func (a *App) forwardTo(pid int, ev uv.MouseEvent) {
	p := a.sup.Get(pid)
	if p == nil {
		return
	}
	r, ok := a.paneRect(pid)
	if !ok {
		return
	}
	inner, _ := model.PaneContentRect(r, a.st.PaneBorders)
	pixel := p.MousePixelMode()
	cw, ch := a.cellW, a.cellH
	if pixel {
		if pw, ph := p.CellPixels(); pw > 0 && ph > 0 {
			cw, ch = pw, ph
		}
	}
	if pixel && (cw <= 0 || ch <= 0) {
		a.ShowToast("Unsupported", "pixel mouse requires known pixel geometry", "error", -1)
		return
	}
	tr := func(m uv.Mouse) uv.Mouse {
		if pixel && a.mousePixel != nil {
			m.X = a.mousePixel.X - inner.X*a.cellW
			m.Y = a.mousePixel.Y - inner.Y*a.cellH
			return m
		}
		return paneMouseCoord(inner, pixel, cw, ch, m)
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

// paneMouseCoord converts a host cell coordinate to a pane-inner
// coordinate. In pixel mode the result is the center pixel of the target
// cell, sized from the known cell geometry. The caller rejects unknown geometry.
func paneMouseCoord(inner layout.Rect, pixel bool, cw, ch int, m uv.Mouse) uv.Mouse {
	x, y := m.X-inner.X, m.Y-inner.Y
	if pixel {
		x = x*cw + cw/2
		y = y*ch + ch/2
	}
	return uv.Mouse{X: x, Y: y, Button: m.Button, Mod: m.Mod}
}

func (a *App) rightClickPass(m uv.Mouse) bool {
	spec := a.sidebarConfig.UI.RightClickPassthroughModifier
	if spec == "" || spec == "off" {
		return false
	}
	chord, err := config.ParseChord(spec + "+x")
	return err == nil && m.Mod == chord.Key.Mod
}
