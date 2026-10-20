package server

import (
	"github.com/kazuph/ikesu/internal/agentview"
	"github.com/kazuph/ikesu/internal/api"
)

func (s *Server) agentViewRequest(req *api.Request) api.Response {
	if req.Method == "agent.view.set" {
		var view agentview.View
		if err := req.DecodeParams(&view); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		if err := view.Validate(); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		s.app.SetAgentView(&view)
	} else {
		var p struct {
			Source *string `json:"source"`
		}
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		current := s.app.State().AgentView
		if p.Source == nil || current != nil && current.Source == *p.Source {
			s.app.SetAgentView(nil)
		}
	}
	view := s.app.State().AgentView
	var source, label *string
	if view != nil {
		source = &view.Source
		label = view.Label
	}
	result := map[string]any{"type": "agent_view", "active": view != nil}
	if view != nil {
		result["source"] = source
		if label != nil {
			result["label"] = label
		}
	}
	return api.Success(req.ID, result)
}
