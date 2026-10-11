package model

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/layout"
)

// --- command-diff snapshot (domain events) ---

// PaneRef records where a pane lived when a StateSnap was taken.
type PaneRef struct {
	WSIdx  int
	TabIdx int
	WSID   WorkspaceID
	TabID  int
}

// TabSnap captures the diffable parts of a tab.
type TabSnap struct {
	ID      int
	Name    string
	Focused PaneID
	FP      string // layout tree fingerprint (structure+ratios+focus)
	Panes   int
}

// WsSnap captures the diffable parts of a workspace.
type WsSnap struct {
	Worktree       *api.WorkspaceWorktreeInfo
	GroupCollapsed bool
	ID             WorkspaceID
	Name           string
	Cwd            string
	ActiveTab      int
	Tabs           []TabSnap
	Panes          int
}

// StateSnap is a lightweight pre/post command snapshot used by the server
// to derive domain events (created/closed/renamed/focused/moved/updated).
type StateSnap struct {
	Active      int
	Workspaces  []WsSnap
	Panes       map[PaneID]PaneRef
	FocusedPane PaneID
}

// Capture records the current state for post-command diffing.
func (s *AppState) Capture() *StateSnap {
	snap := &StateSnap{
		Active:      s.Active,
		Panes:       map[PaneID]PaneRef{},
		FocusedPane: s.FocusedPane(),
	}
	for i, ws := range s.Workspaces {
		wss := WsSnap{Worktree: ws.Worktree, GroupCollapsed: ws.GroupCollapsed, ID: ws.ID, Name: ws.Name, Cwd: ws.Cwd, ActiveTab: ws.ActiveTab}
		for j, t := range ws.Tabs {
			wss.Tabs = append(wss.Tabs, TabSnap{
				ID: t.ID, Name: t.Name, Focused: t.Layout.Focused(),
				FP: t.Layout.Fingerprint(), Panes: t.Layout.PaneCount(),
			})
			for _, pid := range t.Layout.PaneIDs() {
				snap.Panes[pid] = PaneRef{WSIdx: i, TabIdx: j, WSID: ws.ID, TabID: t.ID}
			}
			wss.Panes += t.Layout.PaneCount()
		}
		snap.Workspaces = append(snap.Workspaces, wss)
	}
	return snap
}

// PaneRects returns display pane rects for the tab: a zoomed pane occupies
// the whole area (SPEC G1 zoom), otherwise the layout's normal split rects.
// API layout snapshots still use Layout.Panes (zoomed reports the tree).
func (t *Tab) PaneRects(area Rect) []layout.PaneRect {
	if t.Zoomed != 0 && t.Layout.HasPane(t.Zoomed) {
		return []layout.PaneRect{{ID: t.Zoomed, Rect: area, IsFocused: true}}
	}
	return t.Layout.Panes(area)
}

// PaneRectsForDisplay shares divider borders when pane gaps are disabled.
// The logical split tree remains unchanged for API layout and navigation.
func (s *AppState) PaneRectsForDisplay(t *Tab, area Rect) []layout.PaneRect {
	out := t.PaneRects(area)
	if s.PaneGaps || !s.PaneBorders || t.Zoomed != 0 {
		return out
	}
	for i := range out {
		r := &out[i].Rect
		if r.X+r.W < area.X+area.W {
			r.W++
		}
		if r.Y+r.H < area.Y+area.H {
			r.H++
		}
	}
	return out
}
