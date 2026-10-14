package server

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/herdrimport"
	"github.com/kazuph/goterrace/internal/session"
)

func (s *Server) importHerdr(req *api.Request) api.Response {
	var args api.SessionImportHerdrParams
	if err := req.DecodeParams(&args); err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	}
	p, err := herdrimport.Load(args.Source, s.cfg)
	if err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	}
	if args.Digest == "" || p.Digest != args.Digest || p.Running != args.HerdrRunning {
		return api.Failure(req.ID, api.ErrInvalidRequest, "herdr source changed; preview and confirm again")
	}
	// No session observation is needed for the existing panes: app records
	// remain in place, and their live PTYs must not be restarted.
	prev := s.app.State().Capture()
	merged, added := session.Append(s.app.State().Snapshot(), p.File)
	if err := s.app.AppendSession(merged, added); err != nil {
		return api.Failure(req.ID, "import_failed", err.Error())
	}
	s.afterExec(nil, prev)
	if !p.Running {
		added.EachPane(func(_ *session.Workspace, _ *session.Tab, x *session.Pane) {
			if x.Agent != "" {
				if cmd, ok := s.buildResumeCommand(x.Agent, x.SessionKind, x.SessionID); ok {
					s.typeResume(x.ID, cmd)
				}
			}
		})
	}
	s.saveSession()
	return api.Success(req.ID, struct {
		Type         string `json:"type"`
		Workspaces   int    `json:"workspaces"`
		HerdrRunning bool   `json:"herdr_running"`
		Message      string `json:"message"`
	}{"session_imported", len(added.Workspaces), p.Running, fmt.Sprintf("added %d workspaces", len(added.Workspaces))})
}
