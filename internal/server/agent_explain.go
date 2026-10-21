package server

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/detect"
)

func (s *Server) agentExplain(id, target string) api.Response {
	t, err := s.resolveTarget(target, true)
	if err != nil {
		return api.Failure(id, err.Code, err.Message)
	}
	p := s.sup.Get(t.pid)
	if p == nil {
		return s.paneNotFound(id, target)
	}
	p.PollProcess()
	sn := p.Snapshot()
	if sn.AgentID == "" {
		return api.Failure(id, "agent_explain_unavailable", fmt.Sprintf("agent target %s does not have a detected agent label", target))
	}
	e := detect.Explain(sn.AgentID, detect.Input{Screen: p.ScreenText(), OSCTitle: sn.OSCTitle, OSCProgress: sn.OSCProgress})
	return api.Success(id, api.AgentExplainResult{Type: "agent_explain", Explain: e})
}
