package client

import (
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
)

func TestGuardApprovalVisibilityAndDismissal(t *testing.T) {
	now := time.Unix(100, 0)
	guard := api.Decision{DecisionID: "d1", Kind: "command_guard", Status: "pending", Origin: &api.DecisionOrigin{PaneID: "p1"}}
	projection := &endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{guard}}
	var state approvalState
	state.update(projection, "p1", now, time.Time{})
	if state.open != "d1" {
		t.Fatalf("inactive input should show decision, got %q", state.open)
	}
	state.dismiss()
	state.update(projection, "p1", now, time.Time{})
	if state.open != "" {
		t.Fatal("dismissed decision reopened without user action")
	}
	state.openPane("p1")
	if state.open != "d1" {
		t.Fatal("manual reopen failed")
	}
	state.update(&endpoint.DecisionProjection{BootID: "a", Revision: 2}, "p1", now, time.Time{})
	if state.open != "" || len(state.pending) != 0 {
		t.Fatal("resolved decision remained visible")
	}
}

func TestGuardApprovalWaitsForPaneActivationAfterInput(t *testing.T) {
	now := time.Unix(100, 0)
	guard := api.Decision{DecisionID: "d1", Kind: "command_guard", Status: "pending", Origin: &api.DecisionOrigin{PaneID: "p1"}}
	projection := &endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{guard}}
	var state approvalState
	state.update(projection, "p1", now, now)
	if state.open != "" {
		t.Fatal("typing opened approval")
	}
	state.update(projection, "p1", now.Add(11*time.Second), now)
	if state.open != "" {
		t.Fatal("elapsed time alone opened approval")
	}
	state.focus("p2")
	state.focus("p1")
	if state.open != "d1" {
		t.Fatal("returning to pane did not open approval")
	}
}
