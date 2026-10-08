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
