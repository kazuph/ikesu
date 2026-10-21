package server

import (
	"encoding/json"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestAgentExplainAPI(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	waitAPISocket(t, dir)
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()
	var created api.WorkspaceCreatedResult
	apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Focus: true}, &created)
	target := created.RootPane.PaneID
	// Existing gtx target resolution requires detected/reported identity.
	response := apiReq(t, dir, api.MethodAgentExplain, api.AgentTarget{Target: target})
	if response.Err == nil || response.Err.Code != api.ErrAgentNotFound {
		t.Fatal(response)
	}
	name := "diagnostic"
	apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: target, Source: "test", Agent: "codex", State: "idle"}, nil)
	apiOK(t, dir, api.MethodAgentRename, api.AgentRenameParams{Target: target, Name: &name}, nil)
	response = apiReq(t, dir, api.MethodAgentExplain, api.AgentTarget{Target: name})
	if response.Err != nil {
		t.Fatal(response)
	}
	var e api.AgentExplainResult
	if json.Unmarshal(response.Result, &e) != nil || e.Type != "agent_explain" || e.Explain.Agent != "codex" {
		t.Fatal(string(response.Result))
	}
	response = apiReq(t, dir, api.MethodAgentExplain, api.AgentTarget{Target: "missing"})
	if response.Err == nil || response.Err.Code != api.ErrAgentNotFound {
		t.Fatal(response)
	}
}
