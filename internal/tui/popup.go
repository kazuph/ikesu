package tui

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"os"
)

// 80% is the distribution's observed omitted-size default (viewport 36
// for a 48-row terminal area); its generated config also documents this size.
const popupDefaultPercent = 80

func popupInner(r layout.Rect) layout.Rect {
	// The observed popup leaves one inner column outside the terminal grid.
	return layout.Rect{X: r.X + 1, Y: r.Y + 1, W: max(1, r.W-3), H: max(1, r.H-2)}
}

func (a *App) OpenPopup(cwd, command string, width, height *api.PopupSize) (string, error) {
	w := a.st.ActiveWorkspace()
	if w == nil {
		return "", fmt.Errorf("no active workspace")
	}
	dimension := func(size *api.PopupSize) api.PopupSize {
		if size != nil {
			return *size
		}
		return api.PopupSize{Percent: popupDefaultPercent}
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	a.lastSpawnError = nil
	a.exec(model.CmdOpenPopup{WorkspaceID: w.ID, Cwd: cwd, Argv: []string{shell, "-c", command}, Width: dimension(width), Height: dimension(height)})
	if a.lastSpawnError != nil {
		err := a.lastSpawnError
		a.exec(model.CmdClosePopup{})
		return "", err
	}
	return a.st.Popup.TerminalID, nil
}

func (a *App) PopupTerminal(target string) *api.PopupTerminalResult {
	pop := a.st.Popup
	if pop == nil || target != pop.TerminalID {
		return nil
	}
	p := a.sup.Get(pop.PaneID)
	if p == nil {
		return nil
	}
	sn := p.Snapshot()
	rows := 0
	p.WithEmu(func(e *vt.Emulator) { rows = e.Height() })
	return &api.PopupTerminalResult{Type: "popup_terminal", TerminalID: pop.TerminalID, ContentRevision: sn.Revision, Scroll: api.PaneScrollInfo{OffsetFromBottom: a.scroll[pop.PaneID], MaxOffsetFromBottom: sn.ScrollbackLen, ViewportRows: rows}}
}

func (a *App) ScrollPopup(target string, offset uint64) *api.PopupTerminalResult {
	info := a.PopupTerminal(target)
	if info == nil {
		return nil
	}
	a.setScrollOff(a.st.Popup.PaneID, int(min(offset, uint64(info.Scroll.MaxOffsetFromBottom))))
	return a.PopupTerminal(target)
}

func (a *App) PopupSelectionText(anchor, cursor api.PaneTextPoint) string {
	if a.st.Popup == nil {
		return ""
	}
	s := &selState{pane: a.st.Popup.PaneID, ax: int(anchor.Col), ay: int(anchor.Row), hx: int(cursor.Col), hy: int(cursor.Row)}
	text, _ := a.selText(s)
	return text
}

func (a *App) popupVisibilityChanged(wasVisible bool) {
	visible := a.st.PopupVisible()
	if wasVisible && !visible && a.st.Popup != nil {
		pid := a.st.Popup.PaneID
		if a.sel != nil && a.sel.pane == pid {
			a.popupSel, a.sel = a.sel, nil
		}
		if a.cm != nil && a.cm.pane == pid {
			a.popupCopy, a.cm = a.cm, nil
		}
		if a.fwd == pid {
			a.fwd = -1
		}
		if a.sb != nil && a.sb.pane == pid {
			a.sb = nil
		}
		a.popupAutoScroll = 0
	}
	if !wasVisible && visible {
		a.sel, a.popupSel = a.popupSel, nil
		a.cm, a.popupCopy = a.popupCopy, nil
		if a.sel != nil {
			a.sel.inner = a.innerRect(a.st.Popup.PaneID)
		}
	}
}

func (a *App) clearPopupInteraction(pid int) {
	if a.sel != nil && a.sel.pane == pid {
		a.sel = nil
		a.popupAutoScroll = 0
	}
	if a.popupSel != nil && a.popupSel.pane == pid {
		a.popupSel = nil
	}
	if a.popupCopy != nil && a.popupCopy.pane == pid {
		a.popupCopy = nil
	}
	if a.fwd == pid {
		a.fwd = -1
	}
	if a.sb != nil && a.sb.pane == pid {
		a.sb = nil
	}
}

func (a *App) popupMouse(ev input.Event) bool {
	if !a.st.PopupVisible() {
		return false
	}
	pop := a.st.Popup
	m := ev.Mouse
	if ev.Kind == input.KindMouseClick && m.Button == uv.MouseLeft && !pointIn(m.X, m.Y, a.st.PopupRect()) {
		a.exec(model.CmdClosePopup{})
		return true
	}
	if a.fwd == pop.PaneID {
		a.forwardMouse(ev)
		return true
	}
	if a.sel != nil && a.sel.pane == pop.PaneID {
		if ev.Kind == input.KindMouseWheel {
			a.wheel(m)
			return true
		}
		inner := a.sel.inner
		a.popupAutoScroll = 0
		if m.Y < inner.Y {
			a.popupAutoScroll = 1
		} else if m.Y >= inner.Y+inner.H {
			a.popupAutoScroll = -1
		}
		a.selMouse(ev)
		if ev.Kind == input.KindMouseRelease {
			a.popupAutoScroll = 0
		}
		return true
	}
	if !pointIn(m.X, m.Y, a.st.PanesArea()) && !pointIn(m.X, m.Y, a.st.PopupRect()) {
		return false
	}
	inner := a.innerRect(pop.PaneID)
	if !pointIn(m.X, m.Y, inner) {
		return true
	}
	p := a.sup.Get(pop.PaneID)
	if p == nil {
		return true
	}
	switch ev.Kind {
	case input.KindMouseWheel:
		if p.Snapshot().MouseTracking {
			a.forwardTo(pop.PaneID, uv.MouseWheelEvent(m))
		} else {
			a.wheel(m)
		}
	case input.KindMouseClick:
		if p.Snapshot().MouseTracking {
			a.forwardTo(pop.PaneID, uv.MouseClickEvent(m))
			a.fwd = pop.PaneID
		} else if m.Button == uv.MouseLeft {
			x, y := m.X-inner.X, viewTop(p, a.scroll[pop.PaneID], inner.H)+m.Y-inner.Y
			a.sel = &selState{pane: pop.PaneID, ax: x, ay: y, hx: x, hy: y, inner: inner, scrollAt: a.scroll[pop.PaneID]}
		}
	}
	return true
}

func (a *App) popupSelectionAutoScroll() {
	if !a.st.PopupVisible() || a.sel == nil || a.sel.pane != a.st.Popup.PaneID || a.popupAutoScroll == 0 {
		return
	}
	// Use the existing terminal wheel step on the existing UI tick cadence.
	a.setScrollOff(a.sel.pane, a.scroll[a.sel.pane]+3*a.popupAutoScroll)
	if p := a.sup.Get(a.sel.pane); p != nil {
		top := viewTop(p, a.scroll[a.sel.pane], a.sel.inner.H)
		a.sel.hy = top
		if a.popupAutoScroll < 0 {
			a.sel.hy += a.sel.inner.H - 1
		}
		a.sel.moved = true
		a.dirty = true
	}
}
