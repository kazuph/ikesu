package model

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/layout"
)

// --- internals ---

func (s *AppState) allocPane() PaneID {
	s.nextPaneID = max(s.nextPaneID, s.nextRuntimePaneID) + 1
	s.nextRuntimePaneID = s.nextPaneID
	return s.nextPaneID
}

func (s *AppState) allocTab() *Tab {
	s.nextTabID++
	return &Tab{ID: s.nextTabID, Layout: layout.New(s.allocPane())}
}

func (s *AppState) newWorkspace(cwd string) *Workspace {
	s.nextWorkspaceID++
	ws := &Workspace{
		ID:   s.nextWorkspaceID,
		Name: fmt.Sprintf("workspace %d", s.nextWorkspaceID),
		Cwd:  cwd,
	}
	ws.Tabs = []*Tab{s.allocTab()}
	s.Workspaces = append(s.Workspaces, ws)
	return ws
}

// TabLabel is the display name: the stored name or "tab N".
func (t *Tab) TabLabel() string {
	if t.Name != "" {
		return t.Name
	}
	return fmt.Sprintf("tab %d", t.ID)
}

func (s *AppState) closeWorkspace(idx int) []Effect {
	if idx < 0 || idx >= len(s.Workspaces) {
		return nil
	}
	ws := s.Workspaces[idx]
	var effs []Effect
	if s.Popup != nil && s.Popup.WorkspaceID == ws.ID {
		effs = append(effs, s.closePopup()...)
	}
	for _, t := range ws.Tabs {
		for _, id := range t.Layout.PaneIDs() {
			effs = append(effs, EffKillPane{Pane: id})
		}
	}
	s.Workspaces = append(s.Workspaces[:idx], s.Workspaces[idx+1:]...)
	if len(s.Workspaces) == 0 {
		return append(effs, EffQuit{})
	}
	if s.Active > idx {
		s.Active--
	}
	if s.Active >= len(s.Workspaces) {
		s.Active = len(s.Workspaces) - 1
	}
	return append(effs, EffSyncLayout{})
}

func (s *AppState) closeTab(wi, ti int) []Effect {
	if wi < 0 || wi >= len(s.Workspaces) {
		return nil
	}
	ws := s.Workspaces[wi]
	if ti < 0 || ti >= len(ws.Tabs) {
		return nil
	}
	var effs []Effect
	for _, id := range ws.Tabs[ti].Layout.PaneIDs() {
		effs = append(effs, EffKillPane{Pane: id})
	}
	ws.Tabs = append(ws.Tabs[:ti], ws.Tabs[ti+1:]...)
	if len(ws.Tabs) == 0 {
		// The last tab of a workspace takes the workspace with it.
		s.Workspaces = append(s.Workspaces[:wi], s.Workspaces[wi+1:]...)
		if len(s.Workspaces) == 0 {
			return append(effs, EffQuit{})
		}
		if s.Active > wi {
			s.Active--
		}
		if s.Active >= len(s.Workspaces) {
			s.Active = len(s.Workspaces) - 1
		}
		return append(effs, EffSyncLayout{})
	}
	if ws.ActiveTab > ti {
		ws.ActiveTab--
	}
	if ws.ActiveTab >= len(ws.Tabs) {
		ws.ActiveTab = len(ws.Tabs) - 1
	}
	return append(effs, EffSyncLayout{})
}

func (s *AppState) closePane(pane PaneID) []Effect {
	if s.Overlay != nil && pane == s.Overlay.PaneID {
		return s.closeOverlay()
	}
	wi, ti := s.FindPane(pane)
	if wi < 0 {
		return nil
	}
	ws := s.Workspaces[wi]
	var effs []Effect
	effs = append(effs, EffKillPane{Pane: pane})
	if ws.Tabs[ti].Zoomed == pane {
		ws.Tabs[ti].Zoomed = 0
	}
	if !ws.Tabs[ti].Layout.Close(pane) {
		// Last pane of the tab closes the tab; the last tab takes the
		// workspace (and possibly the app) with it.
		return append(effs, s.closeTab(wi, ti)...)
	}
	return append(effs, EffSyncLayout{})
}

// zoomPane implements CmdZoomPane on the pane's own tab.
func (s *AppState) zoomPane(t *Tab, pane PaneID, mode string, out *ZoomOutcome) {
	if out == nil {
		out = &ZoomOutcome{}
	}
	single := t.Layout.PaneCount() <= 1
	if single {
		// A one-pane tab can never zoom: the op only (re)focuses the pane
		// and reports why nothing else happened.
		fc := t.Layout.Focused() != pane
		if fc {
			t.Layout.Focus(pane)
		}
		*out = ZoomOutcome{Changed: fc, FocusChanged: fc, Reason: "single_pane"}
		return
	}
	zoomed := t.Zoomed == pane
	anyZoom := t.Zoomed != 0
	switch mode {
	case "on":
		if zoomed {
			*out = ZoomOutcome{Changed: false, Zoomed: true, Reason: "already_zoomed"}
			return
		}
		t.Zoomed = pane
		fc := t.Layout.Focused() != pane
		t.Layout.Focus(pane)
		*out = ZoomOutcome{Changed: true, ZoomChanged: true, FocusChanged: fc, Zoomed: true}
	case "off":
		if !anyZoom {
			*out = ZoomOutcome{Changed: false, Reason: "already_unzoomed"}
			return
		}
		t.Zoomed = 0
		*out = ZoomOutcome{Changed: true, ZoomChanged: true}
	case "", "toggle":
		if zoomed {
			t.Zoomed = 0
			*out = ZoomOutcome{Changed: true, ZoomChanged: true}
			return
		}
		t.Zoomed = pane
		fc := t.Layout.Focused() != pane
		t.Layout.Focus(pane)
		*out = ZoomOutcome{Changed: true, ZoomChanged: true, FocusChanged: fc, Zoomed: true}
	}
}

// movePane implements CmdMovePane: the pane keeps its id and process while
// its leaf slot migrates to another tab/workspace.
func (s *AppState) movePane(c CmdMovePane) []Effect {
	wi, ti := s.FindPane(c.Pane)
	if wi < 0 {
		return nil
	}
	src := s.Workspaces[wi].Tabs[ti]
	out := c.Out
	if out == nil {
		out = &PaneMoveOutcome{}
	}
	if out != nil {
		out.Pane = c.Pane
		out.PrevWsID, out.PrevTabID = s.Workspaces[wi].ID, src.ID
		out.TgtWsIdx, out.TgtTabIdx, out.NewWsIdx = -1, -1, -1
	}
	if src.Zoomed != 0 {
		if out != nil {
			out.Reason = "zoomed_tab"
		}
		return nil
	}
	switch c.Dest.Kind {
	case MoveDestTab:
		if c.Dest.TabW < 0 || c.Dest.TabW >= len(s.Workspaces) {
			return nil
		}
		dws := s.Workspaces[c.Dest.TabW]
		if c.Dest.TabT < 0 || c.Dest.TabT >= len(dws.Tabs) {
			return nil
		}
		dst := dws.Tabs[c.Dest.TabT]
		if dst == src {
			if out != nil {
				out.Reason = "same_tab"
			}
			return nil
		}
		if dst.Zoomed != 0 {
			if out != nil {
				out.Reason = "zoomed_tab"
			}
			return nil
		}
		dwi, dti := c.Dest.TabW, c.Dest.TabT
		s.dropPaneFromTab(wi, ti, c.Pane, out)
		if out != nil && out.ClosedWsID != 0 && dwi > wi {
			dwi--
		} else if out != nil && out.ClosedTabID != 0 && dwi == wi && dti > ti {
			// Source tab dissolved inside the destination workspace.
			dti--
		}
		target := c.Dest.Target
		if !dst.Layout.HasPane(target) {
			target = dst.Layout.LastLeaf()
		}
		ratio := c.Dest.Ratio
		if ratio <= 0 {
			ratio = 0.5
		}
		dst.Layout.SplitAt(target, c.Dest.Split, c.Pane, ratio, false)
		if c.Focus {
			dst.Layout.Focus(c.Pane)
		}
		if out != nil {
			out.Changed = true
			out.TgtWsID, out.TgtTabID = dws.ID, dst.ID
			out.TgtWsIdx, out.TgtTabIdx = dwi, dti
		}
		s.finishMoveFocus(c, out)
		return []Effect{EffSyncLayout{}}
	case MoveDestNewTab:
		dw := c.Dest.Workspace
		if dw < 0 {
			dw = wi
		}
		if dw < 0 || dw >= len(s.Workspaces) {
			return nil
		}
		s.dropPaneFromTab(wi, ti, c.Pane, out)
		if out != nil && out.ClosedWsID != 0 && dw > wi {
			dw--
		}
		ws := s.Workspaces[dw]
		tab := s.allocTabFrom(c.Pane)
		tab.Name = c.Dest.Label
		ws.Tabs = append(ws.Tabs, tab)
		if out != nil {
			out.Changed = true
			out.NewTabID = tab.ID
			out.TgtWsID, out.TgtTabID = ws.ID, tab.ID
			out.TgtWsIdx, out.TgtTabIdx = dw, len(ws.Tabs)-1
		}
		s.finishMoveFocus(c, out)
		return []Effect{EffSyncLayout{}}
	case MoveDestNewWorkspace:
		s.dropPaneFromTab(wi, ti, c.Pane, out)
		s.nextWorkspaceID++
		ws := &Workspace{
			ID:   s.nextWorkspaceID,
			Name: c.Dest.Label,
			Cwd:  c.Dest.Cwd,
		}
		if ws.Name == "" {
			ws.Name = fmt.Sprintf("workspace %d", ws.ID)
		}
		tab := s.allocTabFrom(c.Pane)
		tab.Name = c.Dest.TabLabel
		ws.Tabs = []*Tab{tab}
		s.Workspaces = append(s.Workspaces, ws)
		if out != nil {
			out.Changed = true
			out.NewWsID, out.NewTabID = ws.ID, tab.ID
			out.NewWsIdx = len(s.Workspaces) - 1
			out.TgtWsID, out.TgtTabID = ws.ID, tab.ID
			out.TgtWsIdx, out.TgtTabIdx = out.NewWsIdx, 0
		}
		s.finishMoveFocus(c, out)
		return []Effect{EffSyncLayout{}}
	}
	return nil
}

// dropPaneFromTab removes pane's leaf from its tab without killing the
// process; an emptied tab/workspace is removed from the model and recorded
// in out.
func (s *AppState) dropPaneFromTab(wi, ti int, pane PaneID, out *PaneMoveOutcome) {
	ws := s.Workspaces[wi]
	ws.Tabs[ti].Layout.RemoveLeaf(pane)
	if len(ws.Tabs[ti].Layout.PaneIDs()) > 0 {
		return
	}
	// Empty tab dissolves; an emptied workspace dissolves with it.
	ws.Tabs = append(ws.Tabs[:ti], ws.Tabs[ti+1:]...)
	if out != nil {
		out.ClosedTabID = out.PrevTabID
	}
	if len(ws.Tabs) == 0 {
		s.Workspaces = append(s.Workspaces[:wi], s.Workspaces[wi+1:]...)
		if out != nil {
			out.ClosedWsID = out.PrevWsID
		}
		return
	}
	if ws.ActiveTab >= len(ws.Tabs) {
		ws.ActiveTab = len(ws.Tabs) - 1
	}
}

// finishMoveFocus applies the focus flag: focus=true activates the
// destination workspace and tab; otherwise the current selection survives
// (indices may have shifted if a workspace closed).
func (s *AppState) finishMoveFocus(c CmdMovePane, out *PaneMoveOutcome) {
	if c.Focus && out != nil && out.TgtWsIdx >= 0 {
		s.Active = out.TgtWsIdx
		s.Workspaces[out.TgtWsIdx].ActiveTab = out.TgtTabIdx
	}
	if len(s.Workspaces) == 0 {
		return
	}
	if s.Active >= len(s.Workspaces) {
		s.Active = len(s.Workspaces) - 1
	}
	if !c.Focus {
		ws := s.Workspaces[s.Active]
		if ws.ActiveTab >= len(ws.Tabs) {
			ws.ActiveTab = len(ws.Tabs) - 1
		}
	}
}

// allocTabFrom wraps an existing pane id in a fresh single-pane tab.
func (s *AppState) allocTabFrom(pane PaneID) *Tab {
	s.nextTabID++
	return &Tab{ID: s.nextTabID, Layout: layout.Restore(layout.LeafNode(pane), pane, []int{pane})}
}

// applyLayout implements CmdApplyLayout: optionally close the target tab,
// then append a new tab whose tree is built from the spec, spawning a
// process per leaf.
func (s *AppState) applyLayout(c CmdApplyLayout) []Effect {
	if c.Workspace < 0 || c.Workspace >= len(s.Workspaces) || c.Root == nil {
		return nil
	}
	wsCwd := s.Workspaces[c.Workspace].Cwd
	var effs []Effect
	wi := c.Workspace
	if c.Tab >= 0 {
		effs = append(effs, s.closeTab(wi, c.Tab)...)
		if wi >= len(s.Workspaces) {
			// The replaced tab was its workspace's last: recreate an
			// otherwise-empty workspace so the applied tab has a home.
			// closeTab emitted EffQuit for the empty workspace set; the
			// apply keeps the app alive, so drop it.
			kept := effs[:0]
			for _, e := range effs {
				if _, quit := e.(EffQuit); !quit {
					kept = append(kept, e)
				}
			}
			effs = kept
			s.nextWorkspaceID++
			s.Workspaces = append(s.Workspaces, &Workspace{
				ID:   s.nextWorkspaceID,
				Name: fmt.Sprintf("workspace %d", s.nextWorkspaceID),
				Cwd:  wsCwd,
			})
			wi = len(s.Workspaces) - 1
		}
	}
	ws := s.Workspaces[wi]
	s.nextTabID++
	root, ok := s.buildApplyTree(c.Root, &effs, ws.Cwd)
	if !ok {
		return nil
	}
	var ids []int
	collectApplyLeaves(root, &ids)
	if len(ids) == 0 {
		return nil
	}
	tab := &Tab{ID: s.nextTabID, Name: c.TabLabel, Layout: layout.Restore(root, ids[0], ids)}
	ws.Tabs = append(ws.Tabs, tab)
	tabIdx := len(ws.Tabs) - 1
	if c.Out != nil {
		c.Out.WsIdx = wi
		c.Out.TabIdx = tabIdx
		c.Out.PaneIDs = ids
	}
	if c.Focus {
		s.Active = wi
		ws.ActiveTab = tabIdx
	}
	return append(effs, EffSyncLayout{})
}

// buildApplyTree allocates a pane id per leaf and queues its spawn effect.
func (s *AppState) buildApplyTree(n *ApplyNode, effs *[]Effect, wsCwd string) (*layout.Node, bool) {
	if n == nil {
		return nil, false
	}
	if n.Leaf != nil {
		id := s.allocPane()
		cwd := n.Leaf.Cwd
		if cwd == "" {
			cwd = wsCwd
		}
		*effs = append(*effs, EffSpawnPane{Pane: id, Cwd: cwd, Env: n.Leaf.Env, Argv: n.Leaf.Argv})
		if n.Leaf.Label != "" {
			if s.PaneLabels == nil {
				s.PaneLabels = map[PaneID]string{}
			}
			s.PaneLabels[id] = n.Leaf.Label
		}
		return layout.LeafNode(id), true
	}
	first, ok1 := s.buildApplyTree(n.First, effs, wsCwd)
	second, ok2 := s.buildApplyTree(n.Second, effs, wsCwd)
	if !ok1 || !ok2 {
		return nil, false
	}
	return layout.SplitNode(n.Dir, n.Ratio, first, second), true
}

func collectApplyLeaves(n *layout.Node, out *[]int) {
	if n == nil {
		return
	}
	if n.Leaf >= 0 {
		*out = append(*out, n.Leaf)
		return
	}
	collectApplyLeaves(n.First, out)
	collectApplyLeaves(n.Second, out)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
