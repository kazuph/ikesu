package endpoint

import "github.com/kazuph/ikesu/internal/model"

// ViewSelection is terminal-owned navigation retained across a connection loss.
// Pane focus remains shared for viewers displaying the same tab.
type ViewSelection struct {
	Workspace int         `json:"workspace"`
	Tabs      map[int]int `json:"tabs"`
	Pane      int         `json:"pane"`
}

func CaptureSelection(st *model.AppState) *ViewSelection {
	if st == nil {
		return nil
	}
	selection := &ViewSelection{Tabs: map[int]int{}, Pane: st.FocusedPane()}
	if ws := st.ActiveWorkspace(); ws != nil {
		selection.Workspace = ws.ID
	}
	for _, ws := range st.Workspaces {
		if tab := ws.TabRef(); tab != nil {
			selection.Tabs[ws.ID] = tab.ID
		}
	}
	return selection
}
