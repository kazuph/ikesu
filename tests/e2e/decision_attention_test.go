package e2e

import (
	"github.com/kazuph/ikesu/internal/api"
	"strings"
	"testing"
	"time"
)

func TestDecisionAttentionLifecycleAndLiveHandoff(t *testing.T) {
	s := start(t)
	s.waitFor("shell ready", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	var before api.PaneInfoResult
	s.rpc(api.MethodPaneGet, api.PaneTarget{PaneID: "p1"}, &before)
	var created api.DecisionResult
	s.rpc(api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "ask", Title: "Attention", Origin: &api.DecisionOrigin{PaneID: "p1"}, Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}}, &created)
	var pane api.PaneInfoResult
	s.rpc(api.MethodPaneGet, api.PaneTarget{PaneID: "p1"}, &pane)
	if pane.Pane.AgentStatus != "blocked" {
		t.Fatal(pane)
	}
	if out, err := s.cli("server", "live-handoff"); err != nil {
		t.Fatal(out, err)
	}
	s.rpc(api.MethodPaneGet, api.PaneTarget{PaneID: "p1"}, &pane)
	if pane.Pane.AgentStatus != "blocked" {
		t.Fatal("handoff lost attention", pane)
	}
	s.rpc(api.MethodDecisionCancel, api.DecisionTargetParams{DecisionID: created.Decision.DecisionID}, nil)
	s.rpc(api.MethodPaneGet, api.PaneTarget{PaneID: "p1"}, &pane)
	if pane.Pane.AgentStatus != before.Pane.AgentStatus {
		t.Fatal("underlying state changed", before, pane)
	}
	timeout := uint64(1)
	s.rpc(api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "ask", Title: "Expiry", Origin: &api.DecisionOrigin{PaneID: "p1"}, Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}, TimeoutMS: &timeout}, &created)
	s.rpc(api.MethodDecisionWait, api.DecisionWaitParams{DecisionID: created.Decision.DecisionID}, nil)
	s.rpc(api.MethodPaneGet, api.PaneTarget{PaneID: "p1"}, &pane)
	if pane.Pane.AgentStatus == "blocked" {
		t.Fatal("expiry retained attention", pane)
	}
	s.rpc(api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "ask", Title: "Restart", Origin: &api.DecisionOrigin{PaneID: "p1"}, Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}}, &created)
	if out, err := s.cli("server", "restart"); err != nil {
		t.Fatal(out, err)
	}
	s.waitFor("restart attention", 10*time.Second, func(_ string) bool {
		s.rpc(api.MethodPaneGet, api.PaneTarget{PaneID: "p1"}, &pane)
		return pane.Pane.AgentStatus == "blocked"
	})
	yes := "yes"
	s.rpc(api.MethodDecisionAnswer, api.DecisionAnswerParams{DecisionID: created.Decision.DecisionID, OptionID: &yes}, nil)
	s.rpc(api.MethodPaneGet, api.PaneTarget{PaneID: "p1"}, &pane)
	if pane.Pane.AgentStatus == "blocked" {
		t.Fatal("answer retained attention", pane)
	}
}
