package server

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
)

func (s *Server) sidebarRequest(req *api.Request) api.Response {
	switch req.Method {
	case "workspace.set_section":
		var p struct {
			WorkspaceID string        `json:"workspace_id"`
			Section     model.Section `json:"section"`
		}
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		if !p.Section.Valid() {
			return api.Failure(req.ID, api.ErrInvalidRequest, "invalid workspace section")
		}
		i := s.findWorkspace(p.WorkspaceID)
		if i < 0 {
			return s.wsNotFound(req.ID, p.WorkspaceID)
		}
		s.app.ExecCmd(model.CmdSetSection{Index: i, Section: p.Section})
		return api.Success(req.ID, api.WorkspaceInfoResult{Type: "workspace_info", Workspace: s.wsInfo(i)})
	case "workspace.duplicate":
		var p struct {
			WorkspaceID string `json:"workspace_id"`
			Focus       bool   `json:"focus"`
		}
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		i := s.findWorkspace(p.WorkspaceID)
		if i < 0 {
			return s.wsNotFound(req.ID, p.WorkspaceID)
		}
		idx, err := s.app.DuplicateWorkspace(i, p.Focus)
		if err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return api.Success(req.ID, api.WorkspaceInfoResult{Type: "workspace_info", Workspace: s.wsInfo(idx)})
	}
	return api.Failure(req.ID, api.ErrInvalidRequest, "unsupported sidebar operation")
}
