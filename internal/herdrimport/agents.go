package herdrimport

import "fmt"

func resolveRef(w workspace, ti, id int, p pane, l ledger, s snapshot) (*ref, string) {
	if p.Session != nil && p.Session.Value != "" {
		return p.Session, ""
	}
	agent := p.ManagedAgent
	if agent == "" {
		agent = p.AgentName
	}
	var candidates []*ref
	tabID := fmt.Sprintf("%s:t%d", w.ID, w.TabNumbers[ti])
	for _, r := range l.Entries {
		if r.Pane == id && r.Workspace == w.ID && r.Tab == tabID && r.Cwd == p.Cwd && (agent == "" || agent == r.Agent) {
			candidates = append(candidates, &ref{Agent: r.Agent, Kind: "id", Value: r.ID, Source: r.Source})
		}
	}
	for _, r := range s.Agents {
		if r.Pane == fmt.Sprintf("p%d", id) && r.Workspace == w.ID && r.Cwd == p.Cwd && (agent == "" || agent == r.Agent) {
			candidates = append(candidates, &ref{Agent: r.Agent, Kind: "id", Value: r.ID, Source: "snapshot"})
		}
	}
	var found *ref
	for _, r := range candidates {
		if r.Value == "" {
			continue
		}
		if found != nil && (found.Agent != r.Agent || found.Value != r.Value) {
			return &ref{Agent: agent}, "保存記録の会話 ID が矛盾：shell として復元"
		}
		found = r
	}
	if found != nil {
		return found, ""
	}
	if p.Session != nil {
		agent = p.Session.Agent
	}
	if agent != "" {
		return &ref{Agent: agent, Kind: "id"}, "会話 ID 不明：shell として復元"
	}
	return nil, ""
}
