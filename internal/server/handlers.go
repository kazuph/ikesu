package server

import (
	"fmt"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
)

// dispatchOnLoop runs on the app goroutine; it may touch st/sup safely.
func (s *Server) dispatchOnLoop(req *api.Request) api.Response {
	switch req.Method {
	case api.MethodWorkspaceList:
		var p api.EmptyParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.wsList(req.ID)
	case api.MethodWorkspaceGet:
		var p api.WorkspaceTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.wsGet(req.ID, p.WorkspaceID)
	case api.MethodWorkspaceCreate:
		var p api.WorkspaceCreateParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.wsCreate(req.ID, p)
	case api.MethodWorkspaceFocus:
		var p api.WorkspaceTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.wsFocus(req.ID, p.WorkspaceID)
	case api.MethodWorkspaceRename:
		var p api.WorkspaceRenameParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.wsRename(req.ID, p)
	case api.MethodWorkspaceClose:
		var p api.WorkspaceTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.wsClose(req.ID, p.WorkspaceID)

	case api.MethodTabList:
		var p api.TabListParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.tabList(req.ID, p)
	case api.MethodTabGet:
		var p api.TabTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.tabGet(req.ID, p.TabID)
	case api.MethodTabCreate:
		var p api.TabCreateParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.tabCreate(req.ID, p)
	case api.MethodTabFocus:
		var p api.TabTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.tabFocus(req.ID, p.TabID)
	case api.MethodTabRename:
		var p api.TabRenameParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.tabRename(req.ID, p)
	case api.MethodTabClose:
		var p api.TabTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.tabClose(req.ID, p.TabID)

	case api.MethodPaneList:
		var p api.PaneListParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneList(req.ID, p)
	case api.MethodPaneGet:
		var p api.PaneTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneGet(req.ID, p.PaneID)
	case api.MethodPaneCurrent:
		var p api.PaneCurrentParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneCurrent(req.ID, p)
	case api.MethodPaneFocus:
		var p api.PaneFocusParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneFocus(req.ID, p.PaneID)
	case api.MethodPaneRename:
		var p api.PaneRenameParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneRename(req.ID, p)
	case api.MethodPaneRead:
		var p api.PaneReadParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneRead(req.ID, p)
	case api.MethodPaneSplit:
		var p api.PaneSplitParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneSplit(req.ID, p)
	case api.MethodPaneClose:
		var p api.PaneTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneClose(req.ID, p.PaneID)
	case api.MethodPaneSendText:
		var p api.PaneSendTextParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneSendText(req.ID, p)
	case api.MethodPaneSendKeys:
		var p api.PaneSendKeysParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneSendKeys(req.ID, p)
	case api.MethodPaneSendInput:
		var p api.PaneSendInputParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneSendInput(req.ID, p)
	case api.MethodPaneReportAgent:
		var p api.PaneReportAgentParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneReportAgent(req.ID, p)

	case api.MethodMsgSend:
		var p api.MsgSendParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.msgSend(req.ID, p)
	case api.MethodMsgInbox:
		var p api.MsgInboxParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.msgInbox(req.ID, p)
	case api.MethodMsgHistory:
		var p api.MsgHistoryParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.msgHistory(req.ID, p)
	case api.MethodMsgRooms:
		var p api.EmptyParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.msgRooms(req.ID)

	case api.MethodAgentList:
		var p api.EmptyParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.agentList(req.ID)
	case api.MethodAgentGet:
		var p api.AgentTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.agentGet(req.ID, p.Target)
	case api.MethodAgentFocus:
		var p api.AgentTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.agentFocus(req.ID, p.Target)
	case api.MethodAgentRead:
		var p api.AgentReadParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.agentRead(req.ID, p)
	case api.MethodAgentSend:
		var p api.AgentSendParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.agentSend(req.ID, p)
	case api.MethodAgentStart:
		var p api.AgentStartParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.agentStart(req.ID, p)
	case api.MethodAgentRename:
		var p api.AgentRenameParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.agentRename(req.ID, p)

	case api.MethodRunStart:
		var p api.RunStartParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.runStart(req.ID, p)
	case api.MethodRunCancel:
		var p api.RunCancelParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.runCancel(req.ID, p)
	}
	return api.Failure(req.ID, api.ErrUnknownMethod,
		fmt.Sprintf("unknown method %q", req.Method))
}

// --- lookups ---

// findPane locates a pane by public id, returning workspace and tab indices.
func (s *Server) findPane(paneRef string) (pid, wsIdx, tabIdx int, ok bool) {
	n, valid := parsePaneID(paneRef)
	if !valid {
		return 0, -1, -1, false
	}
	wsIdx, tabIdx = s.app.State().FindPane(n)
	return n, wsIdx, tabIdx, wsIdx >= 0
}

// findTab resolves a public "wN:tM" id to indices.
func (s *Server) findTab(ref string) (wsIdx, tabIdx int) {
	wsID, tabID, ok := parseTabID(ref)
	if !ok {
		return -1, -1
	}
	for i, ws := range s.app.State().Workspaces {
		if ws.ID != wsID {
			continue
		}
		for j, t := range ws.Tabs {
			if t.ID == tabID {
				return i, j
			}
		}
	}
	return -1, -1
}

// findWorkspace resolves a public workspace id to its index.
func (s *Server) findWorkspace(ref string) int {
	id, ok := parseWorkspaceID(ref)
	if !ok {
		return -1
	}
	for i, ws := range s.app.State().Workspaces {
		if ws.ID == id {
			return i
		}
	}
	return -1
}

func (s *Server) paneNotFound(id, ref string) api.Response {
	return api.Failure(id, api.ErrPaneNotFound, fmt.Sprintf("pane not found: %s", ref))
}

func (s *Server) wsNotFound(id, ref string) api.Response {
	return api.Failure(id, api.ErrWorkspaceNotFound, fmt.Sprintf("workspace not found: %s", ref))
}

// --- info builders ---

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// wsAgentStatus aggregates pane statuses: blocked > working > done > idle.
func (s *Server) wsAgentStatus(ws *model.Workspace) api.AgentStatus {
	best := api.AgentUnknown
	for _, t := range ws.Tabs {
		best = maxAgent(best, s.tabAgentStatus(t))
	}
	return best
}

func (s *Server) tabAgentStatus(t *model.Tab) api.AgentStatus {
	best := api.AgentUnknown
	for _, pid := range t.Layout.PaneIDs() {
		best = maxAgent(best, s.app.PaneStatusKey(pid))
	}
	return best
}

func maxAgent(a, b api.AgentStatus) api.AgentStatus {
	prio := map[string]int{"idle": 1, "done": 2, "working": 3, "blocked": 4}
	if prio[string(b)] > prio[string(a)] {
		return b
	}
	return a
}

func (s *Server) wsInfo(i int) api.WorkspaceInfo {
	st := s.app.State()
	ws := st.Workspaces[i]
	wsID := fmtWorkspaceID(ws.ID)
	activeTabID := ""
	if t := ws.TabRef(); t != nil {
		activeTabID = fmtTabID(ws.ID, t.ID)
	}
	nextTab := st.PeekNextTabID()
	return api.WorkspaceInfo{
		NextPublicTabNumber: &nextTab,
		WorkspaceID:         wsID,
		Number:              i + 1,
		Label:               ws.Name,
		Focused:             i == st.Active,
		PaneCount:           ws.PaneCount(),
		TabCount:            len(ws.Tabs),
		ActiveTabID:         activeTabID,
		AgentStatus:         s.wsAgentStatus(ws),
	}
}

// tabInfo builds the public TabInfo for workspace/tab indices.
func (s *Server) tabInfo(wsIdx, tabIdx int) api.TabInfo {
	st := s.app.State()
	ws := st.Workspaces[wsIdx]
	t := ws.Tabs[tabIdx]
	return api.TabInfo{
		TabID:       fmtTabID(ws.ID, t.ID),
		WorkspaceID: fmtWorkspaceID(ws.ID),
		Label:       t.Name,
		Number:      tabIdx + 1,
		Focused:     wsIdx == st.Active && ws.ActiveTab == tabIdx,
		PaneCount:   t.Layout.PaneCount(),
		AgentStatus: s.tabAgentStatus(t),
	}
}

func (s *Server) paneInfo(pid int) (api.PaneInfo, bool) {
	st := s.app.State()
	wsIdx, tabIdx := st.FindPane(pid)
	if wsIdx < 0 {
		return api.PaneInfo{}, false
	}
	ws := st.Workspaces[wsIdx]
	tab := ws.Tabs[tabIdx]
	wsID := fmtWorkspaceID(ws.ID)

	var sn runtime.Snapshot
	if p := s.app.Supervisor().Get(pid); p != nil {
		sn = p.Snapshot()
	}
	agent := sn.AgentID
	if rep, _, ok := s.app.Reported(pid); ok && rep != "" {
		agent = rep
	}
	cwd := sn.Cwd
	if cwd == "" {
		cwd = ws.Cwd
	}
	rows := 0
	for _, pr := range tab.Layout.Panes(st.PanesArea()) {
		if pr.ID == pid {
			rows = max(pr.Rect.H-2, 1)
		}
	}
	return api.PaneInfo{
		PaneID:      fmtPaneID(pid),
		WorkspaceID: wsID,
		TabID:       fmtTabID(ws.ID, tab.ID),
		TerminalID:  fmtPaneID(pid),
		Focused: wsIdx == st.Active && ws.ActiveTab == tabIdx &&
			tab.Layout.Focused() == pid,
		AgentStatus:   s.app.PaneStatusKey(pid),
		Agent:         optString(agent),
		Cwd:           optString(cwd),
		ForegroundCwd: optString(sn.Cwd),
		Label:         optString(st.PaneLabels[pid]),
		TerminalTitle: optString(sn.OSCTitle),
		Revision:      sn.Revision,
		Scroll: &api.PaneScrollInfo{
			OffsetFromBottom:    s.app.ScrollOffset(pid),
			MaxOffsetFromBottom: sn.ScrollbackLen,
			ViewportRows:        rows,
		},
	}, true
}

// --- workspace handlers ---
