package tui

import (
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/render"
	"time"
)

func (v *Viewer) onInput(ui *App, event input.Event) {
	if event.Kind == input.KindFocus {
		v.focusEpoch++
		v.focusUntil = time.Now().Add(time.Second) // SPEC G11: at most one second
	}
	if time.Now().Before(v.focusUntil) && (event.Kind == input.KindKey || event.Kind == input.KindPaste) {
		v.heldKeys = append(v.heldKeys, event)
		return
	}
	ui.onInput(event)
}

func (v *Viewer) releaseKeys(ui *App) {
	v.focusUntil = time.Time{}
	for _, event := range v.heldKeys {
		ui.onInput(event)
	}
	v.heldKeys = nil
}

// SetSnapshotSink switches only this attached viewer to client composition.
func (v *Viewer) SetSnapshotSink(sink func(render.Input) error) {
	v.app.snapshotSink = sink
}

func (v *Viewer) Redraw(a *App) { v.with(a, func(ui *App) { ui.dirty = true }) }

func (v *Viewer) AwaitingFocusRender() bool { return !v.focusUntil.IsZero() }

func (v *Viewer) FocusEpoch() uint64 {
	if v.AwaitingFocusRender() {
		return v.focusEpoch
	}
	return 0
}

// Rendered uses the client-composed hit regions for the next input event.
// A frame drawn before the latest focus return cannot release held keys.
func (v *Viewer) Rendered(a *App, out render.Output, focusEpoch uint64) {
	if v.AwaitingFocusRender() && focusEpoch != v.focusEpoch {
		return
	}
	v.with(a, func(ui *App) {
		wsID, tabID := 0, 0
		if ws := ui.st.ActiveWorkspace(); ws != nil {
			wsID = ws.ID
			if tab := ws.TabRef(); tab != nil {
				tabID = tab.ID
			}
		}
		ui.tabViewWorkspace, ui.tabViewActive = wsID, tabID
		ui.tabViewWidth = ui.st.TabBarArea().W
		ui.tabScroll, ui.tabScrollLeft, ui.tabScrollRight = out.TabScroll, out.TabScrollLeft, out.TabScrollRight
		ui.tabLeftRect, ui.tabRightRect = out.TabLeftRect, out.TabRightRect
		ui.tabHits, ui.newTabRect = out.TabHits, out.NewTabRect
		ui.toastRect, ui.actionBar = out.ToastRect, out.ActionBar
		if ui.help != nil {
			ui.help.rect = out.HelpRect
		}
		v.releaseKeys(ui)
	})
}
