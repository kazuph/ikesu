package tui

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/benchstage"
	"reflect"
)

func (a *App) refreshPaneMetadata() {
	defer benchstage.Span("ui.metadata")()
	if a.paneMetadataSource == nil {
		return
	}
	if a.metadataViews == nil {
		a.metadataViews = map[int]api.PaneMetadataView{}
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
