package model

import (
	"github.com/kazuph/goterrace/internal/layout"
)

// Apply mutates the state for cmd and returns the effects the runtime must
// perform, in order.
func (s *AppState) Apply(cmd Command) (effects []Effect) {
	if s.overlayLayoutMutation(cmd) {
		return nil
	}
	defer func() {
		effects = append(effects, s.syncPopupOwner()...)
		if s.Overlay != nil {
			wi, ti := s.FindPane(s.Overlay.TargetPaneID)
			if wi < 0 || s.Workspaces[wi].ID != s.Overlay.WorkspaceID || s.Workspaces[wi].Tabs[ti].ID != s.Overlay.TabID {
				effects = append(effects, s.closeOverlay()...)
			}
		}
	}()
	if s.OverlayVisible() {
		switch cmd.(type) {
		case CmdFocusDir, CmdCyclePane, CmdFocusPane:
			return nil
		}
	}
	switch c := cmd.(type) {
	case CmdOpenOverlay:
		return s.openOverlay(c)
	case CmdCloseOverlay:
		return s.closeOverlay()
	case CmdOpenPopup:
		return s.openPopup(c)
	case CmdClosePopup:
		if c.Pane != 0 {
			return s.closePopupPane(s.PopupByPane(c.Pane))
		}
		return s.closePopup()
	case CmdPopupResizeLock:
		if s.Popup != nil {
			s.Popup.ResizeLocked = c.Locked
		}
		return nil
	case CmdSetSection:
		if c.Index >= 0 && c.Index < len(s.Workspaces) {
			s.Workspaces[c.Index].Section = c.Section
			delete(s.CollapsedSections, c.Section)
		}
		return nil
	case CmdToggleSection:
		if s.CollapsedSections == nil {
			s.CollapsedSections = map[Section]bool{}
		}
		s.CollapsedSections[c.Section] = !s.CollapsedSections[c.Section]
		return nil
	case CmdSidebarPreset:
		s.SetSidebarPreset(c.Preset)
		return []Effect{EffSyncLayout{}}
	case CmdDuplicateWorkspace:
		return s.duplicateWorkspace(c)
	case CmdToggleWorktreeGroup:
		if c.Index >= 0 && c.Index < len(s.Workspaces) {
			s.Workspaces[c.Index].GroupCollapsed = !s.Workspaces[c.Index].GroupCollapsed
		}
		return nil
	case CmdSetWorktree:
		if c.Index >= 0 && c.Index < len(s.Workspaces) {
			s.Workspaces[c.Index].Worktree = c.Info
		}
		return nil
	case CmdMoveTab:
		return s.moveTab(c)
	case CmdMoveWorkspace:
		return s.moveWorkspace(c.Index, c.InsertIndex)
	case CmdNewWorkspace:
		ws := s.newWorkspace(c.Cwd)
		if c.Name != "" {
			ws.Name, ws.DefaultName = c.Name, true
		}
		ws.Section = c.Section
		delete(s.CollapsedSections, c.Section)
		if !c.NoFocus {
			s.Active = len(s.Workspaces) - 1
		}
		return []Effect{
			EffSpawnPane{Pane: ws.Tabs[0].Layout.Focused(), Cwd: c.Cwd, Env: c.Env},
			EffSyncLayout{},
		}
	case CmdCloseWorkspace:
		return s.closeWorkspace(c.Index)
	case CmdSelectWorkspace:
		if c.Index >= 0 && c.Index < len(s.Workspaces) {
			s.Active = c.Index
			return []Effect{EffSyncLayout{}}
		}
	case CmdSplitFocused:
		ws := s.ActiveWorkspace()
		if ws == nil {
			return nil
		}
		l := ws.ActiveLayout()
		if l == nil {
			return nil
		}
		id := s.allocPane()
		l.SplitFocused(c.Dir, id)
		return []Effect{
			EffSpawnPane{Pane: id, Cwd: ws.Cwd},
			EffSyncLayout{},
		}
	case CmdSplitPane:
		wi, ti := s.FindPane(c.Pane)
		if wi < 0 {
			return nil
		}
		ws := s.Workspaces[wi]
		l := ws.Tabs[ti].Layout
		id := s.allocPane()
		if !l.SplitAt(c.Pane, c.Dir, id, c.Ratio, c.Focus) {
			return nil
		}
		cwd := c.Cwd
		if cwd == "" {
			cwd = ws.Cwd
		}
		return []Effect{
			EffSpawnPane{Pane: id, Cwd: cwd, Env: c.Env, Argv: c.Argv, AgentCommand: c.AgentCommand},
			EffSyncLayout{},
		}
	case CmdRenameWorkspace:
		if c.Index >= 0 && c.Index < len(s.Workspaces) {
			s.Workspaces[c.Index].Name = c.Name
			s.Workspaces[c.Index].CustomName = c.Name != ""
			s.Workspaces[c.Index].DefaultName = false
		}
	case CmdNewTab:
		if c.Workspace < 0 || c.Workspace >= len(s.Workspaces) {
			return nil
		}
		ws := s.Workspaces[c.Workspace]
		tab := s.allocTab()
		ws.Tabs = append(ws.Tabs, tab)
		if !c.NoFocus {
			ws.ActiveTab = len(ws.Tabs) - 1
			s.Active = c.Workspace
		}
		cwd := c.Cwd
		if cwd == "" {
			cwd = ws.Cwd
		}
		return []Effect{
			EffSpawnPane{Pane: tab.Layout.Focused(), Cwd: cwd, Env: c.Env, Argv: c.Argv, AgentCommand: c.AgentCommand},
			EffSyncLayout{},
		}
	case CmdSelectTab:
		if c.Workspace < 0 || c.Workspace >= len(s.Workspaces) {
			return nil
		}
		ws := s.Workspaces[c.Workspace]
		if c.Tab >= 0 && c.Tab < len(ws.Tabs) {
			ws.ActiveTab = c.Tab
			s.Active = c.Workspace
			return []Effect{EffSyncLayout{}}
		}
	case CmdRenameTab:
		if c.Workspace >= 0 && c.Workspace < len(s.Workspaces) {
			ws := s.Workspaces[c.Workspace]
			if c.Tab >= 0 && c.Tab < len(ws.Tabs) {
				ws.Tabs[c.Tab].Name = c.Label
			}
		}
	case CmdCloseTab:
		return s.closeTab(c.Workspace, c.Tab)
	case CmdRenamePane:
		if s.PaneLabels == nil {
			s.PaneLabels = map[PaneID]string{}
		}
		if c.Label == "" {
			delete(s.PaneLabels, c.Pane)
		} else {
			s.PaneLabels[c.Pane] = c.Label
		}
	case CmdClosePane:
		return s.closePane(c.Pane)
	case CmdFocusPane:
		// Focus the pane's own workspace/tab, then the pane.
		if wi, ti := s.FindPane(c.Pane); wi >= 0 {
			ws := s.Workspaces[wi]
			s.Active = wi
			ws.ActiveTab = ti
			ws.Tabs[ti].Layout.Focus(c.Pane)
		}
	case CmdFocusDir:
		if ws := s.ActiveWorkspace(); ws != nil {
			if l := ws.ActiveLayout(); l != nil {
				l.FocusDir(c.Dir, s.PanesArea())
			}
		}
	case CmdCyclePane:
		if ws := s.ActiveWorkspace(); ws != nil {
			if l := ws.ActiveLayout(); l != nil {
				l.Cycle(c.Next)
			}
		}
	case CmdToggleSidebar:
		s.SidebarVisible = !s.SidebarVisible
		return []Effect{EffSyncLayout{}}
	case CmdResize:
		s.Cols, s.Rows = c.Cols, c.Rows
		return []Effect{EffSyncLayout{}}
	case CmdInput:
		return []Effect{EffInput{Pane: c.Pane, Bytes: c.Bytes}}
	case CmdPaneExited:
		return s.closePane(c.Pane)
	case CmdSplitImplicit:
		if c.Workspace < 0 || c.Workspace >= len(s.Workspaces) {
			return nil
		}
		ws := s.Workspaces[c.Workspace]
		if c.Tab < 0 || c.Tab >= len(ws.Tabs) {
			return nil
		}
		l := ws.Tabs[c.Tab].Layout
		id := s.allocPane()
		ratio := c.Ratio
		if ratio <= 0 {
			ratio = 0.5
		}
		if !l.SplitImplicit(c.Dir, id, ratio, c.Focus) {
			return nil
		}
		cwd := c.Cwd
		if cwd == "" {
			cwd = ws.Cwd
		}
		return []Effect{
			EffSpawnPane{Pane: id, Cwd: cwd, Env: c.Env, Argv: c.Argv, AgentCommand: c.AgentCommand},
			EffSyncLayout{},
		}
	case CmdArrange:
		wi, ti := s.FindPane(c.Pane)
		if wi < 0 {
			return nil
		}
		t := s.Workspaces[wi].Tabs[ti]
		switch c.Op {
		case "cycle":
			t.Layout.CyclePresets()
		case "equalize":
			t.Layout.Equalize()
		case "rotate_forward":
			t.Layout.Rotate(true)
		case "rotate_backward":
			t.Layout.Rotate(false)
		case "move_left":
			t.Layout.MoveToSide(c.Pane, layout.NavLeft)
		case "move_right":
			t.Layout.MoveToSide(c.Pane, layout.NavRight)
		case "move_up":
			t.Layout.MoveToSide(c.Pane, layout.NavUp)
		case "move_down":
			t.Layout.MoveToSide(c.Pane, layout.NavDown)
		default:
			return nil
		}
		return []Effect{EffSyncLayout{}}
	case CmdZoomPane:
		wi, ti := s.FindPane(c.Pane)
		if wi < 0 {
			return nil
		}
		t := s.Workspaces[wi].Tabs[ti]
		s.zoomPane(t, c.Pane, c.Mode, c.Out)
		return []Effect{EffSyncLayout{}}
	case CmdResizePane:
		wi, ti := s.FindPane(c.Pane)
		if wi < 0 {
			return nil
		}
		l := s.Workspaces[wi].Tabs[ti].Layout
		changed := l.ResizeDir(c.Pane, c.Dir, c.Amount)
		if c.Out != nil {
			c.Out.Changed = changed
			if !changed {
				c.Out.Reason = "unchanged"
			}
		}
		return []Effect{EffSyncLayout{}}
	case CmdFocusPaneDir:
		wi, ti := s.FindPane(c.Pane)
		if wi < 0 {
			return nil
		}
		l := s.Workspaces[wi].Tabs[ti].Layout
		nb := l.Neighbor(c.Pane, c.Dir, s.PanesArea())
		if c.Out != nil {
			if nb < 0 {
				c.Out.Reason = "no_neighbor"
				c.Out.Focus = l.Focused()
			} else {
				c.Out.Changed = true
				c.Out.Focus = nb
			}
		}
		if nb >= 0 {
			l.Focus(nb)
			return []Effect{EffSyncLayout{}}
		}
	case CmdSwapPanes:
		wiA, tiA := s.FindPane(c.A)
		wiB, tiB := s.FindPane(c.B)
		out := c.Out
		switch {
		case wiA < 0 || wiB < 0:
			if out != nil {
				out.Reason = "not_found"
				out.Target = -1
			}
		case wiA != wiB || tiA != tiB:
			if out != nil {
				out.Reason = "cross_tab"
				out.Target = -1
			}
		case c.A == c.B:
			if out != nil {
				out.Reason = "same_pane"
				out.Target = c.B
			}
		default:
			l := s.Workspaces[wiA].Tabs[tiA].Layout
			l.SwapLeaves(c.A, c.B)
			if out != nil {
				out.Changed = true
				out.Target = c.B
			}
			return []Effect{EffSyncLayout{}}
		}
	case CmdSwapDir:
		wi, ti := s.FindPane(c.Pane)
		if wi < 0 {
			if c.Out != nil {
				c.Out.Reason = "not_found"
				c.Out.Target = -1
			}
			return nil
		}
		l := s.Workspaces[wi].Tabs[ti].Layout
		nb := l.Neighbor(c.Pane, c.Dir, s.PanesArea())
		if c.Out != nil {
			c.Out.Target = nb
			if nb < 0 {
				c.Out.Reason = "no_neighbor"
			} else {
				c.Out.Changed = true
			}
		}
		if nb >= 0 {
			l.SwapLeaves(c.Pane, nb)
			return []Effect{EffSyncLayout{}}
		}
	case CmdMovePane:
		return s.movePane(c)
	case CmdSetSplitRatio:
		if c.Workspace < 0 || c.Workspace >= len(s.Workspaces) {
			return nil
		}
		ws := s.Workspaces[c.Workspace]
		if c.Tab < 0 || c.Tab >= len(ws.Tabs) {
			return nil
		}
		ws.Tabs[c.Tab].Layout.SetRatioAt(c.Path, c.Ratio)
		return []Effect{EffSyncLayout{}}
	case CmdApplyLayout:
		return s.applyLayout(c)
	case CmdQuit:
		return []Effect{EffQuit{}}
	}
	return nil
}
