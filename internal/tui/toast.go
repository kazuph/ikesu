package tui

import (
	"time"

	"github.com/kazuph/goterrace/internal/model"
)

// toastTTL is how long a notification box stays on screen.
const toastTTL = 10 * time.Second

// Isolated PTY cell clocks measured a 1s start delay and 8s visible lifetime.
// Reproduction: tests/parity/prefix-notification-{target-settled,expiry}.steps.
const serverAgentToastDelay = time.Second
const serverAgentToastTTL = 8 * time.Second

// notification.show lasts 3s in the real stage6a toast-lifecycle cell clock.
const explicitNotificationToastTTL = 3 * time.Second

// toastView is one visible notification (SPEC G4): status dot + title,
// body line, and an optional target pane for click/prefix+o navigation.
type toastView struct {
	title, body, kind string
	position          string
	explicit          bool
	dismissed         bool
	target            int // pane id; <0 = no target
	until             time.Time
	showAt            time.Time
	shown             bool
}

// ShowToast displays a notification box. Call on the app loop.
func (a *App) ShowToast(title, body, kind string, target int) {
	now := time.Now()
	a.toastSequence++
	a.toast = &toastView{
		title: title, body: body, kind: kind,
		target: target, until: now.Add(toastTTL), shown: true,
	}
	if a.serverUI && (kind == "finished" || kind == "needs_attention") {
		delay := serverAgentToastDelay
		if value := a.sidebarConfig.UI.Toast.DelaySeconds; value != nil {
			delay = time.Duration(*value) * time.Second
		}
		a.toast.showAt = now.Add(delay)
		a.toast.until = a.toast.showAt.Add(serverAgentToastTTL)
		a.toast.shown = false
	}
	a.dirty = true
}

// DismissToast removes the visible toast. Call on the app loop.
func (a *App) DismissToast() {
	if a.toast != nil {
		a.toast.dismissed = true
		a.toast = nil
		a.dirty = true
	}
}

// SetNavKey sets the rune that, after the prefix key, jumps to the toast
// target ([keys] open_notification_target, default 'o').
func (a *App) SetNavKey(r rune) { a.navKey = r }

// expireToast drops a stale toast; called from onTick.
func (a *App) expireToast() {
	if a.toast == nil {
		return
	}
	now := time.Now()
	if a.toast.dismissed || now.After(a.toast.until) {
		a.toast = nil
		a.dirty = true
	} else if !a.toast.shown && !now.Before(a.toast.showAt) {
		a.toast.shown = true
		a.dirty = true
	}
}

// toastClick hit-tests a click against the visible toast; when it lands,
// we navigate to the toast target and dismiss. Returns true when the
// click was consumed.
func (a *App) toastClick(x, y int) bool {
	if a.toast == nil || a.toast.explicit || !pointIn(x, y, a.toastRect) {
		return false
	}
	a.navToToastTarget()
	a.DismissToast()
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

// ShowNotification preserves an existing toast instead of replacing it.
func (a *App) NotificationBusy() bool {
	a.expireToast()
	return a.toast != nil
}

func (a *App) ShowNotification(title, body, position string) bool {
	if a.NotificationBusy() {
		return false
	}
	a.ShowToast(title, body, "info", -1)
	a.toast.position = position
	a.toast.explicit = true
	a.toast.until = time.Now().Add(explicitNotificationToastTTL)
	return true
}
