package tui

import (
	"time"

	"github.com/kazuph/goterrace/internal/model"
)

// toastTTL is how long a notification box stays on screen.
const toastTTL = 10 * time.Second

// toastView is one visible notification (SPEC G4): status dot + title,
// body line, and an optional target pane for click/prefix+o navigation.
type toastView struct {
	title, body, kind string
	target            int // pane id; <0 = no target
	until             time.Time
}

// ShowToast displays a notification box. Call on the app loop.
func (a *App) ShowToast(title, body, kind string, target int) {
	a.toast = &toastView{
		title: title, body: body, kind: kind,
		target: target, until: time.Now().Add(toastTTL),
	}
	a.dirty = true
}

// DismissToast removes the visible toast. Call on the app loop.
func (a *App) DismissToast() {
	if a.toast != nil {
		a.toast = nil
		a.dirty = true
	}
}

// SetNavKey sets the rune that, after the prefix key, jumps to the toast
// target ([keys] open_notification_target, default 'o').
func (a *App) SetNavKey(r rune) { a.navKey = r }

// expireToast drops a stale toast; called from onTick.
func (a *App) expireToast() {
	if a.toast != nil && time.Now().After(a.toast.until) {
		a.toast = nil
		a.dirty = true
	}
}

// toastClick hit-tests a click against the visible toast; when it lands,
// we navigate to the toast target and dismiss. Returns true when the
// click was consumed.
func (a *App) toastClick(x, y int) bool {
	if a.toast == nil || !pointIn(x, y, a.toastRect) {
		return false
	}
	a.navToToastTarget()
	a.toast = nil
	a.dirty = true
	return true
}

// navToToastTarget switches workspace/tab/focus to the toast's pane.
func (a *App) navToToastTarget() {
	if a.toast == nil || a.toast.target < 0 {
		return
	}
	wsIdx, tabIdx := a.st.FindPane(a.toast.target)
	if wsIdx < 0 {
		return
	}
	if wsIdx != a.st.Active {
		a.exec(model.CmdSelectWorkspace{Index: wsIdx})
	}
	ws := a.st.Workspaces[wsIdx]
	if ws.ActiveTab != tabIdx {
		a.exec(model.CmdSelectTab{Workspace: wsIdx, Tab: tabIdx})
	}
	a.exec(model.CmdFocusPane{Pane: model.PaneID(a.toast.target)})
}
