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
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	return a.OpenPluginPopup(cwd, []string{shell, "-c", command}, nil, width, height)
}
func (a *App) OpenPluginPopup(cwd string, argv []string, env map[string]string, width, height *api.PopupSize) (string, error) {
	if a.ModalBusy() {
		return "", fmt.Errorf("ui_busy")
	}
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
	a.lastSpawnError = nil
	a.exec(model.CmdOpenPopup{WorkspaceID: w.ID, Cwd: cwd, Argv: argv, Env: env, Width: dimension(width), Height: dimension(height)})
	if a.lastSpawnError != nil {
		err := a.lastSpawnError
		a.exec(model.CmdClosePopup{})
		return "", err
	}
	return a.st.Popup.TerminalID, nil
}

func (a *App) PopupTerminal(target string) *api.PopupTerminalResult {
	pop := a.popupForTerminal(target)
	if pop == nil {
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
	a.setScrollOff(a.popupForTerminal(target).PaneID, int(min(offset, uint64(info.Scroll.MaxOffsetFromBottom))))
	return a.PopupTerminal(target)
}

func (a *App) popupForTerminal(target string) *model.PopupPaneState {
	if a.st.Popup != nil && a.st.Popup.TerminalID == target {
		return a.st.Popup
	}
	for _, pop := range a.st.Popups {
		if pop.TerminalID == target {
			return pop
		}
	}
	return nil
}
func (a *App) PopupPaneID(target string) int {
	if pop := a.popupForTerminal(target); pop != nil {
		return pop.PaneID
	}
	return 0
}
func (a *App) PopupSelectionText(target string, anchor, cursor api.PaneTextPoint) string {
	pid := a.PopupPaneID(target)
	if pid == 0 {
		return ""
	}
	text, _ := a.selText(&selState{pane: pid, ax: int(anchor.Col), ay: int(anchor.Row), hx: int(cursor.Col), hy: int(cursor.Row)})
	return text
}

func (a *App) popupVisibilityChanged(wasVisible bool, previous *model.PopupPaneState) {
	visible := a.st.PopupVisible()
	changed := previous != a.st.Popup
	if a.popupSelections == nil {
		a.popupSelections = map[int]*selState{}
		a.popupCopies = map[int]*copyMode{}
	}
	if wasVisible && (!visible || changed) && previous != nil {
		pid := previous.PaneID
		if a.sel != nil && a.sel.pane == pid {
			a.popupSel, a.sel = a.sel, nil
			a.popupSelections[pid] = a.popupSel
		}
		if a.cm != nil && a.cm.pane == pid {
			a.popupCopy, a.cm = a.cm, nil
			a.popupCopies[pid] = a.popupCopy
		}
		if a.fwd == pid {
			a.fwd = -1
		}
		if a.sb != nil && a.sb.pane == pid {
			a.sb = nil
		}
		a.popupAutoScroll = 0
	}
	if (!wasVisible || changed) && visible {
		pid := a.st.Popup.PaneID
		a.sel, a.cm = a.popupSelections[pid], a.popupCopies[pid]
		delete(a.popupSelections, pid)
		delete(a.popupCopies, pid)
		a.popupSel, a.popupCopy = nil, nil
		if a.sel != nil {
			a.sel.inner = a.innerRect(a.st.Popup.PaneID)
		}
	}
}

func (a *App) clearPopupInteraction(pid int) {
	delete(a.popupSelections, pid)
	delete(a.popupCopies, pid)
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
	if !a.st.PopupVisible() && !a.st.OverlayVisible() {
		return false
	}
	pid := 0
	rect := layout.Rect{}
	if a.st.OverlayVisible() {
		pid = a.st.Overlay.PaneID
		rect, _ = a.st.OverlayRect()
	} else {
		pid = a.st.Popup.PaneID
		rect = a.st.PopupRect()
	}
	m := ev.Mouse
	if !a.st.OverlayVisible() && ev.Kind == input.KindMouseClick && m.Button == uv.MouseLeft && !pointIn(m.X, m.Y, rect) {
		a.exec(model.CmdClosePopup{Pane: pid})
		return true
	}
	if a.fwd == pid {
		a.forwardMouse(ev)
		return true
	}
	if a.sel != nil && a.sel.pane == pid {
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
	if !pointIn(m.X, m.Y, a.st.PanesArea()) && !pointIn(m.X, m.Y, rect) {
		return false
	}
	inner := a.innerRect(pid)
	if !pointIn(m.X, m.Y, inner) {
		return true
	}
	p := a.sup.Get(pid)
	if p == nil {
		return true
	}
	if ev.Kind == input.KindMouseClick && a.pluginLinkClick(pid, m) {
		return true
	}
	switch ev.Kind {
	case input.KindMouseWheel:
		if p.Snapshot().MouseTracking {
			a.forwardTo(pid, uv.MouseWheelEvent(m))
		} else {
			a.wheel(m)
		}
	case input.KindMouseClick:
		if p.Snapshot().MouseTracking {
			a.fwdStrip = 0
			a.forwardTo(pid, uv.MouseClickEvent(m))
			a.fwd = pid
		} else if m.Button == uv.MouseLeft && m.Mod == 0 {
			x, y := m.X-inner.X, viewTop(p, a.scroll[pid], inner.H)+m.Y-inner.Y
			a.sel = &selState{pane: pid, ax: x, ay: y, hx: x, hy: y, inner: inner, scrollAt: a.scroll[pid]}
		}
	}
	return true
}

func (a *App) popupSelectionAutoScroll() {
	pid := 0
	if a.st.OverlayVisible() {
		pid = a.st.Overlay.PaneID
	} else if a.st.PopupVisible() {
		pid = a.st.Popup.PaneID
	}
	if pid == 0 || a.sel == nil || a.sel.pane != pid || a.popupAutoScroll == 0 {
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
