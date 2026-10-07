package server

import (
	"fmt"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
)

func (s *Server) tabNotFound(id, ref string) api.Response {
	return api.Failure(id, api.ErrTabNotFound, fmt.Sprintf("tab not found: %s", ref))
}

func (s *Server) tabList(id string, p api.TabListParams) api.Response {
	st := s.app.State()
	out := api.TabListResult{Type: "tab_list", Tabs: []api.TabInfo{}}
	for wi, ws := range st.Workspaces {
		if p.WorkspaceID != nil && *p.WorkspaceID != "" {
			if s.findWorkspace(*p.WorkspaceID) != wi {
				continue
			}
		}
		for ti := range ws.Tabs {
			out.Tabs = append(out.Tabs, s.tabInfo(wi, ti))
		}
	}
	return api.Success(id, out)
}

func (s *Server) tabGet(id, ref string) api.Response {
	wi, ti := s.findTab(ref)
	if wi < 0 {
		return s.tabNotFound(id, ref)
	}
	return api.Success(id, api.TabInfoResult{Type: "tab_info", Tab: s.tabInfo(wi, ti)})
}

func (s *Server) tabCreate(id string, p api.TabCreateParams) api.Response {
	st := s.app.State()
	wsIdx := st.Active
	if p.WorkspaceID != nil && *p.WorkspaceID != "" {
		wsIdx = s.findWorkspace(*p.WorkspaceID)
		if wsIdx < 0 {
			return s.wsNotFound(id, *p.WorkspaceID)
		}
	}
	cwd := ""
	if p.Cwd != nil {
		cwd = *p.Cwd
	}
	if cwd == "" {
		cwd = st.Workspaces[wsIdx].Cwd
	}
	s.app.ExecCmd(model.CmdNewTab{
		Workspace: wsIdx, Cwd: cwd, Env: p.Env, NoFocus: !p.Focus,
	})
	ws := st.Workspaces[wsIdx]
	tabIdx := len(ws.Tabs) - 1
	if p.Label != nil && *p.Label != "" {
		s.app.ExecCmd(model.CmdRenameTab{Workspace: wsIdx, Tab: tabIdx, Label: *p.Label})
	}
	root := ws.Tabs[tabIdx].Layout.Focused()
	info, _ := s.paneInfo(root)
	return api.Success(id, api.TabCreatedResult{
		Type:     "tab_created",
		Tab:      s.tabInfo(wsIdx, tabIdx),
		RootPane: info,
	})
}

func (s *Server) tabFocus(id, ref string) api.Response {
	wi, ti := s.findTab(ref)
	if wi < 0 {
		return s.tabNotFound(id, ref)
	}
	s.app.ExecCmd(model.CmdSelectTab{Workspace: wi, Tab: ti})
	return api.Success(id, api.TabInfoResult{Type: "tab_info", Tab: s.tabInfo(wi, ti)})
}

func (s *Server) tabRename(id string, p api.TabRenameParams) api.Response {
	wi, ti := s.findTab(p.TabID)
	if wi < 0 {
		return s.tabNotFound(id, p.TabID)
	}
	s.app.ExecCmd(model.CmdRenameTab{Workspace: wi, Tab: ti, Label: p.Label})
	return api.Success(id, api.TabInfoResult{Type: "tab_info", Tab: s.tabInfo(wi, ti)})
}

func (s *Server) tabClose(id, ref string) api.Response {
	wi, ti := s.findTab(ref)
	if wi < 0 {
		return s.tabNotFound(id, ref)
	}
	s.app.ExecCmd(model.CmdCloseTab{Workspace: wi, Tab: ti})
	return api.Success(id, api.OkResult{Type: "ok"})
}

// --- pane handlers ---
