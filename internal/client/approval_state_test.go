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

func TestGuardApprovalFiltersAndOrdersPendingDecisions(t *testing.T) {
	now := time.Unix(100, 0)
	pane := &api.DecisionOrigin{PaneID: "p1"}
	var state approvalState
	state.update(&endpoint.DecisionProjection{BootID: "a", Revision: 2, Pending: []api.Decision{
		{DecisionID: "new", Kind: "command_guard", Status: "pending", Origin: pane, CreatedUnixMS: 2},
		{DecisionID: "old", Kind: "command_guard", Status: "pending", Origin: pane, CreatedUnixMS: 1},
		{DecisionID: "ask", Kind: "ask", Status: "pending", Origin: pane},
		{DecisionID: "outside", Kind: "command_guard", Status: "pending", Origin: &api.DecisionOrigin{PaneID: "p1", OutsidePane: true}},
	}}, "p1", now, time.Time{})
	if len(state.pending) != 2 || state.open != "old" {
		t.Fatalf("pending=%v open=%s", state.pending, state.open)
	}
	state.update(&endpoint.DecisionProjection{BootID: "a", Revision: 1}, "p1", now, time.Time{})
	if state.open != "old" || len(state.pending) != 2 {
		t.Fatal("stale projection replaced pending state")
	}
}

func TestGuardApprovalRequiresSelectionBeforeAnswer(t *testing.T) {
	var state approvalState
	state.update(&endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{{
		DecisionID: "d1", Kind: "command_guard", Status: "pending", Origin: &api.DecisionOrigin{PaneID: "p1"},
		Options: []api.DecisionOption{{ID: "approve"}, {ID: "day_allow"}, {ID: "reject"}},
	}}}, "p1", time.Unix(100, 0), time.Time{})
	if state.answer() != nil {
		t.Fatal("Enter without a selection must not answer")
	}
	state.selectOption(2)
	if state.open != "d1" || state.selected != "day_allow" {
		t.Fatal("number must select without answering")
	}
	answer := state.answer()
	if answer == nil || answer.DecisionID != "d1" || answer.OptionID == nil || *answer.OptionID != "day_allow" {
		t.Fatalf("answer=%+v", answer)
	}
}
