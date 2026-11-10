package model

type CmdMoveTab struct{ Workspace, Index, InsertIndex int }

func (CmdMoveTab) isCommand() {}

func (s *AppState) moveTab(c CmdMoveTab) []Effect {
	if c.Workspace != s.Active || c.Workspace < 0 || c.Workspace >= len(s.Workspaces) {
		return nil
	}
	ws := s.Workspaces[c.Workspace]
	if c.Index < 0 || c.Index >= len(ws.Tabs) || c.InsertIndex < 0 || c.InsertIndex > len(ws.Tabs) {
		return nil
	}
	active := ws.Tabs[ws.ActiveTab].ID
	tab := ws.Tabs[c.Index]
	ws.Tabs = append(ws.Tabs[:c.Index], ws.Tabs[c.Index+1:]...)
	to := c.InsertIndex
	if to > c.Index {
		to--
	}
	ws.Tabs = append(ws.Tabs, nil)
	copy(ws.Tabs[to+1:], ws.Tabs[to:])
	ws.Tabs[to] = tab
	for i, t := range ws.Tabs {
		if t.ID == active {
			ws.ActiveTab = i
			break
		}
	}
	return []Effect{EffSyncLayout{}}
}

// CmdMoveTabToWorkspace moves the tab at Index of Workspace to the end of
// workspace Dest. The tab keeps its id, layout and processes. A source
// workspace left without tabs is removed (its popup pane is closed).
type CmdMoveTabToWorkspace struct {
	Workspace, Index, Dest int
	Focus                  bool
}

func (CmdMoveTabToWorkspace) isCommand() {}

func (s *AppState) moveTabToWorkspace(c CmdMoveTabToWorkspace) []Effect {
	if c.Workspace < 0 || c.Workspace >= len(s.Workspaces) || c.Dest < 0 || c.Dest >= len(s.Workspaces) || c.Workspace == c.Dest {
		return nil
	}
	src, dst := s.Workspaces[c.Workspace], s.Workspaces[c.Dest]
	if c.Index < 0 || c.Index >= len(src.Tabs) {
		return nil
	}
	active := s.ActiveWorkspace()
	srcActive := src.Tabs[src.ActiveTab]
	tab := src.Tabs[c.Index]
	var effs []Effect
	src.Tabs = append(src.Tabs[:c.Index], src.Tabs[c.Index+1:]...)
	dst.Tabs = append(dst.Tabs, tab)
	if len(src.Tabs) == 0 {
		if s.Popup != nil && s.Popup.WorkspaceID == src.ID {
			effs = append(effs, s.closePopupPane(s.Popup)...)
		}
		s.Workspaces = append(s.Workspaces[:c.Workspace], s.Workspaces[c.Workspace+1:]...)
	} else {
		src.ActiveTab = min(c.Index, len(src.Tabs)-1) // like closing: the neighbour
		for i, t := range src.Tabs {
			if t == srcActive {
				src.ActiveTab = i
			}
		}
	}
	if c.Focus || active == src && len(src.Tabs) == 0 {
		active = dst
		dst.ActiveTab = len(dst.Tabs) - 1
	}
	for i, w := range s.Workspaces {
		if w == active {
			s.Active = i
		}
	}
	return append(effs, EffSyncLayout{})
}
