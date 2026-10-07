package server

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
)

func (s *Server) wsList(id string) api.Response {
	st := s.app.State()
	out := api.WorkspaceListResult{Type: "workspace_list", Workspaces: []api.WorkspaceInfo{}}
	for i := range st.Workspaces {
		out.Workspaces = append(out.Workspaces, s.wsInfo(i))
	}
	return api.Success(id, out)
}

func (s *Server) wsGet(id, ref string) api.Response {
	i := s.findWorkspace(ref)
	if i < 0 {
		return s.wsNotFound(id, ref)
	}
	return api.Success(id, api.WorkspaceInfoResult{Type: "workspace_info", Workspace: s.wsInfo(i)})
}

func (s *Server) wsCreate(id string, p api.WorkspaceCreateParams) api.Response {
	st := s.app.State()
	cwd := ""
	if p.Cwd != nil {
		cwd = *p.Cwd
	}
	if cwd == "" {
		cwd = s.cwd
	}
	s.app.ExecCmd(model.CmdNewWorkspace{Cwd: cwd, Env: p.Env, NoFocus: !p.Focus})
	idx := len(st.Workspaces) - 1
	ws := st.Workspaces[idx]
	if p.Label != nil && *p.Label != "" {
		s.app.ExecCmd(model.CmdRenameWorkspace{Index: idx, Name: *p.Label})
	}
	root := ws.Tabs[0].Layout.Focused()
	info, _ := s.paneInfo(root)
	tab := api.TabInfo{
		TabID:       fmtTabID(ws.ID, ws.Tabs[0].ID),
		WorkspaceID: fmtWorkspaceID(ws.ID),
		Label:       "",
		Number:      1,
		Focused:     p.Focus,
		PaneCount:   1,
		AgentStatus: api.AgentUnknown,
	}
	return api.Success(id, api.WorkspaceCreatedResult{
		Type:      "workspace_created",
		Workspace: s.wsInfo(idx),
		Tab:       tab,
		RootPane:  info,
	})
}

func (s *Server) wsFocus(id, ref string) api.Response {
	i := s.findWorkspace(ref)
	if i < 0 {
		return s.wsNotFound(id, ref)
	}
	s.app.ExecCmd(model.CmdSelectWorkspace{Index: i})
	return api.Success(id, api.WorkspaceInfoResult{Type: "workspace_info", Workspace: s.wsInfo(i)})
}

func (s *Server) wsRename(id string, p api.WorkspaceRenameParams) api.Response {
	i := s.findWorkspace(p.WorkspaceID)
	if i < 0 {
		return s.wsNotFound(id, p.WorkspaceID)
	}
	s.app.ExecCmd(model.CmdRenameWorkspace{Index: i, Name: p.Label})
	return api.Success(id, api.WorkspaceInfoResult{Type: "workspace_info", Workspace: s.wsInfo(i)})
}

func (s *Server) wsClose(id, ref string) api.Response {
	i := s.findWorkspace(ref)
	if i < 0 {
		return s.wsNotFound(id, ref)
	}
	s.app.ExecCmd(model.CmdCloseWorkspace{Index: i})
	return api.Success(id, api.OkResult{Type: "ok"})
}

// --- tab handlers ---
