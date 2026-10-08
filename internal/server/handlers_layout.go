package server

// Layout-operation API handlers (SPEC G1): pane.layout/neighbor/edges/
// focus_direction/resize/swap/zoom/move/arrange/scroll and
// layout.export/apply/set_split_ratio. Results nest under op-named keys
// inside type-tagged envelopes, matching the herdr wire shape.

import (
	"fmt"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func navDirOf(d string) (layout.NavDir, bool) {
	switch d {
	case "left":
		return layout.NavLeft, true
	case "right":
		return layout.NavRight, true
	case "up":
		return layout.NavUp, true
	case "down":
		return layout.NavDown, true
	}
	return 0, false
}

func apiRect(r layout.Rect) api.PaneLayoutRect {
	return api.PaneLayoutRect{X: r.X, Y: r.Y, Width: r.W, Height: r.H}
}

// splitID mirrors herdr's path-based ids: root is "split_0_root", a
// depth-N child is "split_N_<path-bits>" with 1=second, 0=first.
func splitID(depth int, path []bool) string {
	if len(path) == 0 {
		return fmt.Sprintf("split_%d_root", depth)
	}
	bits := make([]byte, len(path))
	for i, second := range path {
		if second {
			bits[i] = '1'
		} else {
			bits[i] = '0'
		}
	}
	return fmt.Sprintf("split_%d_%s", depth, bits)
}

// layoutSnapshot builds the API snapshot for a tab. Split/pane rects are
// the outer (border-including) rectangles; a zoomed tab still reports the
// full tree (zoom only changes rendering).
func (s *Server) layoutSnapshot(wsIdx, tabIdx int) api.PaneLayoutSnapshot {
	st := s.app.State()
	ws := st.Workspaces[wsIdx]
	t := ws.Tabs[tabIdx]
	area := st.PanesArea()
	snap := api.PaneLayoutSnapshot{
		WorkspaceID:   fmtWorkspaceID(ws.ID),
		TabID:         fmtTabID(ws.ID, t.ID),
		Zoomed:        t.Zoomed != 0,
		Area:          apiRect(area),
		FocusedPaneID: fmtPaneID(t.Layout.Focused()),
	}
	for _, pr := range t.Layout.Panes(area) {
		snap.Panes = append(snap.Panes, api.PaneLayoutPane{
			PaneID:  fmtPaneID(pr.ID),
			Focused: pr.IsFocused,
			Rect:    apiRect(pr.Rect),
		})
	}
	for _, sp := range t.Layout.Splits(area) {
		dir := "right"
		if sp.Direction == layout.Horizontal {
			dir = "down"
		}
		snap.Splits = append(snap.Splits, api.PaneLayoutSplit{
			ID:        splitID(len(sp.Path), sp.Path),
			Direction: dir,
			Ratio:     sp.Ratio,
			Rect:      apiRect(sp.Area),
		})
	}
	return snap
}

// layoutDescription builds the export/apply tree description for a tab.
func (s *Server) layoutDescription(wsIdx, tabIdx int) api.LayoutDescription {
	st := s.app.State()
	ws := st.Workspaces[wsIdx]
	t := ws.Tabs[tabIdx]
	return api.LayoutDescription{
		WorkspaceID:   fmtWorkspaceID(ws.ID),
		TabID:         fmtTabID(ws.ID, t.ID),
		Zoomed:        t.Zoomed != 0,
		FocusedPaneID: fmtPaneID(t.Layout.Focused()),
		Root:          s.nodeDescription(t.Layout.Root()),
	}
}

func (s *Server) nodeDescription(n *layout.Node) *api.LayoutNode {
	if n == nil {
		return nil
	}
	if n.Leaf >= 0 {
		ln := &api.LayoutNode{Type: "pane"}
		pid := fmtPaneID(n.Leaf)
		ln.PaneID = &pid
		if p := s.app.Supervisor().Get(n.Leaf); p != nil {
			if cwd := p.SpawnCwd(); cwd != "" {
				ln.Cwd = &cwd
			}
			if argv := p.SpawnArgv(); len(argv) > 0 {
				ln.Command = argv
			}
		}
		if label := s.app.State().PaneLabels[n.Leaf]; label != "" {
			ln.Label = &label
		}
		return ln
	}
	dir := "right"
	if n.Direction == layout.Horizontal {
		dir = "down"
	}
	return &api.LayoutNode{
		Type:      "split",
		Direction: dir,
		Ratio:     n.Ratio,
		First:     s.nodeDescription(n.First),
		Second:    s.nodeDescription(n.Second),
	}
}

func strp(v string) *string { return &v }

func (s *Server) paneLayout(id string, p api.PaneLayoutParams) api.Response {
	ref := ""
	if p.PaneID != nil {
		ref = *p.PaneID
	}
	if ref == "" && p.TabID != nil {
		wi, ti := s.findTab(*p.TabID)
		if wi < 0 {
			return api.Failure(id, api.ErrTabNotFound, fmt.Sprintf("tab not found: %s", *p.TabID))
		}
		return api.Success(id, api.PaneLayoutResult{Type: "pane_layout", Layout: s.layoutSnapshot(wi, ti)})
	}
	_, wi, ti, ok := s.resolvePane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	return api.Success(id, api.PaneLayoutResult{Type: "pane_layout", Layout: s.layoutSnapshot(wi, ti)})
}

func (s *Server) paneNeighbor(id string, p api.PaneNeighborParams) api.Response {
	dir, ok := navDirOf(p.Direction)
	if !ok {
		return api.Failure(id, api.ErrInvalidRequest, fmt.Sprintf("invalid direction %q", p.Direction))
	}
	ref := ""
	if p.PaneID != nil {
		ref = *p.PaneID
	}
	pid, wi, ti, ok := s.resolvePane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	st := s.app.State()
	l := st.Workspaces[wi].Tabs[ti].Layout
	nb := l.Neighbor(pid, dir, st.PanesArea())
	var nbID *string
	if nb >= 0 {
		nbID = strp(fmtPaneID(nb))
	}
	return api.Success(id, api.PaneNeighborEnvelope{
		Type: "pane_neighbor",
		Neighbor: api.PaneNeighborResult{
			PaneID:         fmtPaneID(pid),
			Direction:      p.Direction,
			NeighborPaneID: nbID,
			Layout:         s.layoutSnapshot(wi, ti),
		},
	})
}

func (s *Server) paneEdges(id string, p api.PaneEdgesParams) api.Response {
	ref := ""
	if p.PaneID != nil {
		ref = *p.PaneID
	}
	pid, wi, ti, ok := s.resolvePane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	st := s.app.State()
	lft, rgt, up, dwn := st.Workspaces[wi].Tabs[ti].Layout.Edges(pid, st.PanesArea())
	return api.Success(id, api.PaneEdgesEnvelope{
		Type: "pane_edges",
		Edges: api.PaneEdgesResult{
			PaneID: fmtPaneID(pid),
			Left:   lft, Right: rgt, Up: up, Down: dwn,
			Layout: s.layoutSnapshot(wi, ti),
		},
	})
}

func (s *Server) paneFocusDirection(id string, p api.PaneFocusDirectionParams) api.Response {
	dir, ok := navDirOf(p.Direction)
	if !ok {
		return api.Failure(id, api.ErrInvalidRequest, fmt.Sprintf("invalid direction %q", p.Direction))
	}
	ref := ""
	if p.PaneID != nil {
		ref = *p.PaneID
	}
	pid, wi, ti, ok := s.resolvePane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	out := &model.FocusDirOutcome{Focus: -1}
	s.app.ExecCmd(model.CmdFocusPaneDir{Pane: pid, Dir: dir, Out: out})
	focused := ""
	if out.Focus >= 0 {
		focused = fmtPaneID(out.Focus)
	}
	return api.Success(id, api.PaneFocusDirectionEnvelope{
		Type: "pane_focus_direction",
		Focus: api.PaneFocusDirectionResult{
			Changed:       out.Changed,
			Reason:        optString(out.Reason),
			SourcePaneID:  fmtPaneID(pid),
			FocusedPaneID: focused,
			Layout:        s.layoutSnapshot(wi, ti),
		},
	})
}

func (s *Server) paneResize(id string, p api.PaneResizeParams) api.Response {
	dir, ok := navDirOf(p.Direction)
	if !ok {
		return api.Failure(id, api.ErrInvalidRequest, fmt.Sprintf("invalid direction %q", p.Direction))
	}
	ref := ""
	if p.PaneID != nil {
		ref = *p.PaneID
	}
	pid, wi, ti, ok := s.resolvePane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	amount := 0.05
	if p.Amount != nil {
		amount = *p.Amount
	}
	out := &model.ResizeOutcome{}
	s.app.ExecCmd(model.CmdResizePane{Pane: pid, Dir: dir, Amount: amount, Out: out})
	st := s.app.State()
	return api.Success(id, api.PaneResizeEnvelope{
		Type: "pane_resize",
		Resize: api.PaneResizeResult{
			Changed:       out.Changed,
			Reason:        optString(out.Reason),
			PaneID:        fmtPaneID(pid),
			FocusedPaneID: fmtPaneID(st.Workspaces[wi].Tabs[ti].Layout.Focused()),
			Layout:        s.layoutSnapshot(wi, ti),
		},
	})
}

func (s *Server) paneSwap(id string, p api.PaneSwapParams) api.Response {
	st := s.app.State()
	out := &model.SwapOutcome{Target: -1}
	var src int
	var wi, ti int
	if p.Direction != nil && *p.Direction != "" {
		dir, ok := navDirOf(*p.Direction)
		if !ok {
			return api.Failure(id, api.ErrInvalidRequest, fmt.Sprintf("invalid direction %q", *p.Direction))
		}
		ref := ""
		if p.PaneID != nil {
			ref = *p.PaneID
		}
		pid, w, t, ok := s.resolvePane(ref)
		if !ok {
			return s.paneNotFound(id, ref)
		}
		src, wi, ti = pid, w, t
		s.app.ExecCmd(model.CmdSwapDir{Pane: pid, Dir: dir, Out: out})
	} else {
		srcRef := ""
		if p.SourcePaneID != nil {
			srcRef = *p.SourcePaneID
		}
		a, w, t, ok := s.resolvePane(srcRef)
		if !ok {
			return s.paneNotFound(id, srcRef)
		}
		if p.TargetPaneID == nil || *p.TargetPaneID == "" {
			return api.Failure(id, api.ErrInvalidRequest,
				"pane.swap needs direction or target_pane_id")
		}
		b, _, _, ok := s.findPane(*p.TargetPaneID)
		if !ok {
			return s.paneNotFound(id, *p.TargetPaneID)
		}
		src, wi, ti = a, w, t
		s.app.ExecCmd(model.CmdSwapPanes{A: a, B: b, Out: out})
	}
	var tgt *string
	if out.Target >= 0 {
		tgt = strp(fmtPaneID(out.Target))
	}
	return api.Success(id, api.PaneSwapEnvelope{
		Type: "pane_swap",
		Swap: api.PaneSwapResult{
			Changed:       out.Changed,
			Reason:        optString(out.Reason),
			SourcePaneID:  fmtPaneID(src),
			TargetPaneID:  tgt,
			FocusedPaneID: fmtPaneID(st.Workspaces[wi].Tabs[ti].Layout.Focused()),
			Layout:        s.layoutSnapshot(wi, ti),
		},
	})
}

func (s *Server) paneZoom(id string, p api.PaneZoomParams) api.Response {
	ref := ""
	if p.PaneID != nil {
		ref = *p.PaneID
	}
	pid, wi, ti, ok := s.resolvePane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	out := &model.ZoomOutcome{}
	s.app.ExecCmd(model.CmdZoomPane{Pane: pid, Mode: p.Mode, Out: out})
	st := s.app.State()
	return api.Success(id, api.PaneZoomEnvelope{
		Type: "pane_zoom",
		Zoom: api.PaneZoomResult{
			Changed:       out.Changed,
			ZoomChanged:   out.ZoomChanged,
			FocusChanged:  out.FocusChanged,
			Reason:        optString(out.Reason),
			PaneID:        fmtPaneID(pid),
			FocusedPaneID: fmtPaneID(st.Workspaces[wi].Tabs[ti].Layout.Focused()),
			Zoomed:        out.Zoomed,
			Layout:        s.layoutSnapshot(wi, ti),
		},
	})
}

func (s *Server) paneArrange(id string, p api.PaneArrangeParams) api.Response {
	pid, wi, ti, ok := s.findPane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	switch p.Arrangement {
	case "equalize", "cycle", "rotate_forward", "rotate_backward",
		"move_left", "move_right", "move_up", "move_down":
	default:
		return api.Failure(id, api.ErrInvalidRequest,
			fmt.Sprintf("invalid arrangement %q", p.Arrangement))
	}
	s.app.ExecCmd(model.CmdArrange{Pane: pid, Op: p.Arrangement})
	return api.Success(id, api.PaneLayoutResult{
		Type:   "pane_layout",
		Layout: s.layoutSnapshot(wi, ti),
	})
}

func (s *Server) paneScroll(id string, p api.PaneScrollParams) api.Response {
	pid, _, _, ok := s.findPane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	s.app.SetScrollOffset(pid, int(p.OffsetFromBottom))
	info, _ := s.paneInfo(pid)
	return api.Success(id, api.PaneInfoResult{Type: "pane_info", Pane: info})
}

func (s *Server) paneMove(id string, p api.PaneMoveParams) api.Response {
	st := s.app.State()
	pid, wi, ti, ok := s.findPane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	dest := model.MoveDest{Kind: -1}
	switch p.Destination.Type {
	case "tab":
		dwi, dti := s.findTab(p.Destination.TabID)
		if dwi < 0 {
			return api.Failure(id, api.ErrTabNotFound,
				fmt.Sprintf("tab not found: %s", p.Destination.TabID))
		}
		var dir layout.Direction
		if p.Destination.Split == nil {
			return api.Failure(id, api.ErrInvalidRequest, "destination.split is required")
		}
		switch *p.Destination.Split {
		case "right", "vertical":
			dir = layout.Vertical
		case "down", "horizontal":
			dir = layout.Horizontal
		default:
			return api.Failure(id, api.ErrInvalidRequest,
				fmt.Sprintf("invalid split direction %q", *p.Destination.Split))
		}
		target := -1
		if p.Destination.TargetPaneID != nil && *p.Destination.TargetPaneID != "" {
			tp, tw, tt, ok := s.findPane(*p.Destination.TargetPaneID)
			if !ok {
				return s.paneNotFound(id, *p.Destination.TargetPaneID)
			}
			if tw != dwi || tt != dti {
				return api.Failure(id, api.ErrInvalidRequest,
					"target_pane_id is not in the destination tab")
			}
			target = tp
		}
		ratio := 0.0
		if p.Destination.Ratio != nil {
			ratio = *p.Destination.Ratio
		}
		dest = model.MoveDest{Kind: model.MoveDestTab, TabW: dwi, TabT: dti,
			Target: target, Split: dir, Ratio: ratio}
	case "new_tab":
		dw := wi
		if p.Destination.WorkspaceID != nil && *p.Destination.WorkspaceID != "" {
			dw = s.findWorkspace(*p.Destination.WorkspaceID)
			if dw < 0 {
				return s.wsNotFound(id, *p.Destination.WorkspaceID)
			}
		}
		dest = model.MoveDest{Kind: model.MoveDestNewTab, Workspace: dw}
		if p.Destination.Label != nil {
			dest.Label = *p.Destination.Label
		}
	case "new_workspace":
		dest = model.MoveDest{Kind: model.MoveDestNewWorkspace}
		if p.Destination.Label != nil {
			dest.Label = *p.Destination.Label
		}
		if p.Destination.TabLabel != nil {
			dest.TabLabel = *p.Destination.TabLabel
		}
		src := st.Workspaces[wi]
		dest.Cwd = src.Cwd
		if pane := s.app.Supervisor().Get(pid); pane != nil && pane.SpawnCwd() != "" {
			dest.Cwd = pane.SpawnCwd()
		}
	default:
		return api.Failure(id, api.ErrInvalidRequest,
			fmt.Sprintf("invalid destination type %q", p.Destination.Type))
	}
	out := &model.PaneMoveOutcome{}
	s.app.ExecCmd(model.CmdMovePane{Pane: pid, Dest: dest, Focus: p.Focus, Out: out})
	res := api.PaneMoveResult{
		Changed:             out.Changed,
		PreviousPaneID:      fmtPaneID(pid),
		PreviousWorkspaceID: fmtWorkspaceID(out.PrevWsID),
		PreviousTabID:       fmtTabID(out.PrevWsID, out.PrevTabID),
		Reason:              optString(out.Reason),
	}
	info, _ := s.paneInfo(pid)
	res.Pane = info
	res.FocusedPaneID = fmtPaneID(st.FocusedPane())
	if out.Changed {
		res.TargetLayout = ptr(s.layoutSnapshot(out.TgtWsIdx, out.TgtTabIdx))
		if dwi, dti := s.findTab(fmtTabID(out.PrevWsID, out.PrevTabID)); dwi >= 0 {
			res.SourceLayout = ptr(s.layoutSnapshot(dwi, dti))
		}
		if out.NewTabID != 0 {
			res.CreatedTab = ptr(s.tabInfo(out.TgtWsIdx, out.TgtTabIdx))
		}
		if out.NewWsID != 0 {
			res.CreatedWorkspace = ptr(s.wsInfo(out.NewWsIdx))
		}
		if out.ClosedTabID != 0 {
			res.ClosedTabID = strp(fmtTabID(out.PrevWsID, out.ClosedTabID))
		}
		if out.ClosedWsID != 0 {
			res.ClosedWorkspaceID = strp(fmtWorkspaceID(out.ClosedWsID))
		}
	} else {
		// Unchanged: target is the pane's own tab.
		res.TargetLayout = ptr(s.layoutSnapshot(wi, ti))
	}
	return api.Success(id, api.PaneMoveEnvelope{Type: "pane_move", MoveResult: res})
}

func ptr[T any](v T) *T { return &v }

func (s *Server) layoutExport(id string, p api.LayoutExportParams) api.Response {
	var wi, ti int
	switch {
	case p.TabID != nil && *p.TabID != "":
		wi, ti = s.findTab(*p.TabID)
		if wi < 0 {
			return api.Failure(id, api.ErrTabNotFound, fmt.Sprintf("tab not found: %s", *p.TabID))
		}
	case p.PaneID != nil && *p.PaneID != "":
		_, w, t, ok := s.findPane(*p.PaneID)
		if !ok {
			return s.paneNotFound(id, *p.PaneID)
		}
		wi, ti = w, t
	default:
		st := s.app.State()
		wi, ti = st.Active, st.Workspaces[st.Active].ActiveTab
	}
	return api.Success(id, api.LayoutExportResult{
		Type:   "layout_export",
		Layout: s.layoutDescription(wi, ti),
	})
}

func (s *Server) layoutSetSplitRatio(id string, p api.LayoutSetSplitRatioParams) api.Response {
	var wi, ti int
	switch {
	case p.TabID != nil && *p.TabID != "":
		wi, ti = s.findTab(*p.TabID)
		if wi < 0 {
			return api.Failure(id, api.ErrTabNotFound, fmt.Sprintf("tab not found: %s", *p.TabID))
		}
	case p.PaneID != nil && *p.PaneID != "":
		_, w, t, ok := s.findPane(*p.PaneID)
		if !ok {
			return s.paneNotFound(id, *p.PaneID)
		}
		wi, ti = w, t
	default:
		st := s.app.State()
		wi, ti = st.Active, st.Workspaces[st.Active].ActiveTab
	}
	st := s.app.State()
	l := st.Workspaces[wi].Tabs[ti].Layout
	if !l.HasSplitPath(p.Path) {
		return api.Failure(id, api.ErrSplitNotFound, "split path not found")
	}
	s.app.ExecCmd(model.CmdSetSplitRatio{Workspace: wi, Tab: ti, Path: p.Path, Ratio: p.Ratio})
	return api.Success(id, api.LayoutSplitRatioSetResult{
		Type:   "layout_split_ratio_set",
		Layout: s.layoutDescription(wi, ti),
	})
}

func (s *Server) layoutApply(id string, p api.LayoutApplyParams) api.Response {
	st := s.app.State()
	var wi, ti = st.Active, -1
	switch {
	case p.TabID != nil && *p.TabID != "":
		w, t := s.findTab(*p.TabID)
		if w < 0 {
			return api.Failure(id, api.ErrTabNotFound, fmt.Sprintf("tab not found: %s", *p.TabID))
		}
		wi, ti = w, t
	case p.WorkspaceID != nil && *p.WorkspaceID != "":
		wi = s.findWorkspace(*p.WorkspaceID)
		if wi < 0 {
			return s.wsNotFound(id, *p.WorkspaceID)
		}
	}
	root, ok := applyNodeOf(p.Root)
	if !ok {
		return api.Failure(id, api.ErrInvalidRequest, "invalid layout root")
	}
	out := &model.ApplyOutcome{}
	tabLabel := ""
	if p.TabLabel != nil {
		tabLabel = *p.TabLabel
	}
	s.app.ExecCmd(model.CmdApplyLayout{
		Workspace: wi, Tab: ti, Root: root,
		TabLabel: tabLabel, Focus: p.Focus, Out: out,
	})
	if len(out.PaneIDs) == 0 {
		return api.Failure(id, api.ErrInvalidRequest, "layout apply produced no panes")
	}
	st = s.app.State()
	ws := st.Workspaces[out.WsIdx]
	tab := ws.Tabs[out.TabIdx]
	desc := api.LayoutDescription{
		WorkspaceID:   fmtWorkspaceID(ws.ID),
		TabID:         fmtTabID(ws.ID, tab.ID),
		Zoomed:        false,
		FocusedPaneID: fmtPaneID(tab.Layout.Focused()),
		Root:          fillAppliedTree(p.Root, out.PaneIDs, ws.Cwd),
	}
	return api.Success(id, api.LayoutApplyResult{Type: "layout_apply", Layout: desc})
}

// applyNodeOf converts the wire tree into model form.
func applyNodeOf(n *api.LayoutNode) (*model.ApplyNode, bool) {
	if n == nil {
		return nil, false
	}
	switch n.Type {
	case "pane":
		leaf := &model.ApplyLeaf{Env: n.Env, Argv: n.Command}
		if n.Cwd != nil {
			leaf.Cwd = *n.Cwd
		}
		if n.Label != nil {
			leaf.Label = *n.Label
		}
		return &model.ApplyNode{Leaf: leaf}, true
	case "split":
		var dir layout.Direction
		switch n.Direction {
		case "right", "vertical":
			dir = layout.Vertical
		case "down", "horizontal":
			dir = layout.Horizontal
		default:
			return nil, false
		}
		first, ok1 := applyNodeOf(n.First)
		second, ok2 := applyNodeOf(n.Second)
		if !ok1 || !ok2 {
			return nil, false
		}
		ratio := n.Ratio
		if ratio <= 0 {
			ratio = 0.5
		}
		return &model.ApplyNode{Dir: dir, Ratio: ratio, First: first, Second: second}, true
	}
	return nil, false
}

// fillAppliedTree returns the request tree with assigned pane ids and
// resolved cwds filled in (the result description echoes the request).
func fillAppliedTree(n *api.LayoutNode, ids []int, wsCwd string) *api.LayoutNode {
	it := 0
	var walk func(n *api.LayoutNode) *api.LayoutNode
	walk = func(n *api.LayoutNode) *api.LayoutNode {
		if n == nil {
			return nil
		}
		out := *n
		if n.Type == "pane" {
			if it < len(ids) {
				out.PaneID = strp(fmtPaneID(ids[it]))
			}
			it++
			if out.Cwd == nil || *out.Cwd == "" {
				out.Cwd = &wsCwd
			}
			return &out
		}
		out.First = walk(n.First)
		out.Second = walk(n.Second)
		return &out
	}
	return walk(n)
}
