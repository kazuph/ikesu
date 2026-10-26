package tui

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/benchstage"
	"reflect"
	"time"
)

func (a *App) refreshPaneMetadata() {
	defer benchstage.Span("ui.metadata")()
	if a.paneMetadataSource == nil {
		return
	}
	if a.metadataViews == nil {
		a.metadataViews = map[int]api.PaneMetadataView{}
	}
	if a.metadataPending != nil {
		now := time.Now()
		if !a.metadataNext.IsZero() && !now.Before(a.metadataNext) {
			a.metadataNext = time.Time{}
			for pid, expires := range a.metadataDeadlines {
				if !now.Before(expires) {
					a.metadataPending[pid] = true
				} else if a.metadataNext.IsZero() || expires.Before(a.metadataNext) {
					a.metadataNext = expires
				}
			}
		}
		if len(a.metadataPending) == 0 {
			return
		}
		for pid := range a.metadataPending {
			if a.metadataExpirySource != nil {
				expires := a.metadataExpirySource(pid)
				if expires.IsZero() {
					delete(a.metadataDeadlines, pid)
				} else {
					a.metadataDeadlines[pid] = expires
				}
			}

			if w, _ := a.st.FindPane(pid); w < 0 {
				delete(a.metadataViews, pid)
			} else {
				view := a.paneMetadataSource(pid)
				if previous, ok := a.metadataViews[pid]; !ok || !reflect.DeepEqual(previous, view) {
					a.metadataViews[pid] = view
					a.dirty = true
				}
			}
			delete(a.metadataPending, pid)
		}
		a.metadataNext = time.Time{}
		for _, expires := range a.metadataDeadlines {
			if a.metadataNext.IsZero() || expires.Before(a.metadataNext) {
				a.metadataNext = expires
			}
		}
		return
	}
	for _, w := range a.st.Workspaces {
		for _, tab := range w.Tabs {
			for _, pid := range tab.Layout.PaneIDs() {
				view := a.paneMetadataSource(pid)
				if previous, ok := a.metadataViews[pid]; !ok || !reflect.DeepEqual(previous, view) {
					a.metadataViews[pid] = view
					a.dirty = true
				}
			}
		}
	}
	for pid := range a.metadataViews {
		if w, _ := a.st.FindPane(pid); w < 0 {
			delete(a.metadataViews, pid)
		}
	}
}

// The session server supplies report/expiry events; standalone sources retain
// their existing polling contract.
func (a *App) UsePaneMetadataEvents(nextExpiry func(int) time.Time) {
	a.metadataPending = map[int]bool{}
	a.metadataDeadlines = map[int]time.Time{}
	a.metadataExpirySource = nextExpiry
}
func (a *App) InvalidatePaneMetadata(pid int) {
	if a.metadataPending != nil {
		a.metadataPending[pid] = true
	}
}
