package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/tui"
)

func (s *Server) worktreeRequest(req *api.Request) api.Response {
	fail := func(err error) api.Response { return api.Failure(req.ID, "worktree_failed", err.Error()) }
	switch req.Method {
	case api.MethodWorktreeList:
		var p api.WorktreeListParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		src, err := s.app.WorktreeSource(valueString(p.Cwd), p.WorkspaceID)
		if err != nil {
			return fail(err)
		}
		items, err := s.app.WorktreeList(src)
		if err != nil {
			return fail(err)
		}
		return api.Success(req.ID, api.WorktreeListResult{Type: "worktree_list", Source: src, Worktrees: items})
	case api.MethodWorktreeCreate:
		var p api.WorktreeCreateParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		out, err := s.app.CreateWorktree(p)
		if err != nil {
			return fail(err)
		}
		w, t, root := s.worktreeParts(out.Index)
		return api.Success(req.ID, api.WorktreeCreatedResult{Type: "worktree_created", Workspace: w, Tab: t, RootPane: root, Worktree: out.Worktree})
	case api.MethodWorktreeOpen:
		var p api.WorktreeOpenParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		out, err := s.app.OpenWorktree(p)
		if err != nil {
			return fail(err)
		}
		w, t, root := s.worktreeParts(out.Index)
		return api.Success(req.ID, api.WorktreeOpenedResult{Type: "worktree_opened", Workspace: w, Tab: t, RootPane: root, Worktree: out.Worktree, AlreadyOpen: out.AlreadyOpen})
	case api.MethodWorktreeRemove:
		var p api.WorktreeRemoveParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		idx := s.findWorkspace(p.WorkspaceID)
		if idx < 0 {
			return s.wsNotFound(req.ID, p.WorkspaceID)
		}
		info, err := s.app.RemoveWorktree(idx, p.Force)
		if err != nil {
			return fail(err)
		}
		return api.Success(req.ID, api.WorktreeRemovedResult{Type: "worktree_removed", WorkspaceID: p.WorkspaceID, Path: info.Path, Forced: p.Force})
	}
	return api.Failure(req.ID, api.ErrUnknownMethod, "unknown worktree method")
}

func valueString(p *string) string {
	if p != nil {
		return *p
	}
	return ""
}

func (s *Server) worktreeParts(i int) (api.WorkspaceInfo, api.TabInfo, api.PaneInfo) {
	ws := s.app.State().Workspaces[i]
	root, _ := s.paneInfo(ws.TabRef().Layout.Focused())
	return s.wsInfo(i), s.tabInfo(i, ws.ActiveTab), root
}

func (s *Server) worktreeEvent(kind string, out tui.WorktreeOutcome, force bool) {
	ws := s.wsInfo(out.Index)
	switch kind {
	case "worktree_created":
		s.emitEvent(kind, ev(kind, "workspace", ws, "worktree", out.Worktree))
	case "worktree_opened":
		s.emitEvent(kind, ev(kind, "workspace", ws, "worktree", out.Worktree, "already_open", out.AlreadyOpen))
	case "worktree_removed":
		s.emitEvent(kind, ev(kind, "workspace", ws, "workspace_id", ws.WorkspaceID, "worktree", out.Worktree, "forced", force))
	}
}

func (s *Server) wsMove(id string, ref string, index int) api.Response {
	i := s.findWorkspace(ref)
	if i < 0 {
		return s.wsNotFound(id, ref)
	}
	if index < 0 || index >= len(s.app.State().Workspaces) {
		return api.Failure(id, api.ErrInvalidRequest, "insert_index is outside the workspace list")
	}
	s.app.ExecCmd(model.CmdMoveWorkspace{Index: i, InsertIndex: index})
	return api.Success(id, s.wsListResult())
}

func (s *Server) wsListResult() api.WorkspaceListResult {
	out := api.WorkspaceListResult{Type: "workspace_list", Workspaces: []api.WorkspaceInfo{}}
	for i := range s.app.State().Workspaces {
		out.Workspaces = append(out.Workspaces, s.wsInfo(i))
	}
	return out
}
