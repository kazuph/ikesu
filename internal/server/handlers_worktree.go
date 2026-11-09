package server

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/tui"
)

func (s *Server) worktreeResultParts(result tui.WorktreeResult) (w api.WorkspaceInfo, t api.TabInfo, root api.PaneInfo, ok bool) {
	s.app.Do(func() {
		for i, ws := range s.app.State().Workspaces {
			if ws.ID == result.WorkspaceID && ws.Generation == result.WorkspaceGeneration && ws.Cwd == result.Outcome.Worktree.Path {
				w, t, root = s.worktreeParts(i)
				ok = true
				return
			}
		}
	})
	return
}

func (s *Server) worktreeRequest(req *api.Request) api.Response {
	fail := func(err error) api.Response { return api.Failure(req.ID, "worktree_failed", err.Error()) }
	switch req.Method {
	case api.MethodWorktreeList:
		var p api.WorktreeListParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		result, err := s.app.ExecuteWorktree(tui.WorktreeRequest{Operation: "list", Cwd: valueString(p.Cwd), WorkspaceID: p.WorkspaceID})
		if err != nil {
			return fail(err)
		}
		return api.Success(req.ID, api.WorktreeListResult{Type: "worktree_list", Source: result.Source, Worktrees: result.Items})
	case api.MethodWorktreeCreate:
		var p api.WorktreeCreateParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		result, err := s.app.ExecuteWorktree(tui.WorktreeRequest{Operation: "create", Cwd: valueString(p.Cwd), WorkspaceID: p.WorkspaceID, Branch: p.Branch, Path: p.Path, Base: p.Base, Label: p.Label, Focus: p.Focus})
		if err != nil {
			return fail(err)
		}
		w, t, root, ok := s.worktreeResultParts(result)
		if !ok {
			return fail(fmt.Errorf("workspace changed after worktree operation"))
		}
		return api.Success(req.ID, api.WorktreeCreatedResult{Type: "worktree_created", Workspace: w, Tab: t, RootPane: root, Worktree: result.Outcome.Worktree})
	case api.MethodWorktreeOpen:
		var p api.WorktreeOpenParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		result, err := s.app.ExecuteWorktree(tui.WorktreeRequest{Operation: "open", Cwd: valueString(p.Cwd), WorkspaceID: p.WorkspaceID, Branch: p.Branch, Path: p.Path, Label: p.Label, Focus: p.Focus})
		if err != nil {
			return fail(err)
		}
		w, t, root, ok := s.worktreeResultParts(result)
		if !ok {
			return fail(fmt.Errorf("workspace changed after worktree operation"))
		}
		return api.Success(req.ID, api.WorktreeOpenedResult{Type: "worktree_opened", Workspace: w, Tab: t, RootPane: root, Worktree: result.Outcome.Worktree, AlreadyOpen: result.Outcome.AlreadyOpen})
	case api.MethodWorktreeRemove:
		var p api.WorktreeRemoveParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		result, err := s.app.ExecuteWorktree(tui.WorktreeRequest{Operation: "remove", WorkspaceID: &p.WorkspaceID, Force: p.Force})
		if err != nil {
			return fail(err)
		}
		return api.Success(req.ID, api.WorktreeRemovedResult{Type: "worktree_removed", WorkspaceID: p.WorkspaceID, Path: result.Removed.Path, Forced: p.Force})
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
