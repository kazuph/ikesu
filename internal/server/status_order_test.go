package server

import (
	"github.com/kazuph/goterrace/internal/api"
	"testing"
)

func TestAggregateStatusOrder(t *testing.T) {
	states := []api.AgentStatus{api.AgentUnknown, api.AgentIdle, api.AgentDone, api.AgentWorking, api.AgentBlocked}
	for i, a := range states {
		for j, b := range states {
			if got := maxAgent(a, b); got != states[max(i, j)] {
				t.Fatalf("aggregate(%s,%s)=%s want=%s", a, b, got, states[max(i, j)])
			}
		}
	}
}
