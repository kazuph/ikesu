package server

import (
	"encoding/json"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
	"time"
)

func TestDecisionOriginAndAttentionOverlay(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-single", "claude")
	s.app.Do(func() {
		st := s.app.State()
		st.Workspaces[0].Name = "Review space"
		st.Workspaces[0].Tabs[0].Name = "Questions"
		st.PaneLabels = map[int]string{1: "Worker"}
		s.app.SetReported(1, "", "")
		before := s.app.PaneStatusKey(1)
		create := func(origin *api.DecisionOrigin) api.Decision {
			resp := s.decisionRequest(&api.Request{ID: "create", Method: api.MethodDecisionCreate, Params: api.MarshalParams(api.DecisionCreateParams{Kind: "ask", Title: "Review?", Origin: origin, Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}})})
			if resp.Err != nil {
				t.Fatal(resp.Err)
			}
			var result api.DecisionResult
			json.Unmarshal(resp.Result, &result)
			return result.Decision
		}
		d := create(&api.DecisionOrigin{PaneID: "p1"})
		if d.Origin.WorkspaceLabel != "Review space" || d.Origin.TabLabel != "Questions" || d.Origin.PaneLabel != "Worker" {
			t.Fatal(d.Origin)
		}
		if s.app.PaneStatusKey(1) != "blocked" {
			t.Fatal("shell not blocked")
		}
		seq := s.app.AgentStateChangeSeq(1)
		second := create(&api.DecisionOrigin{PaneID: "p1", WorkspaceLabel: "Caller space", TabLabel: "Caller tab", PaneLabel: "Caller pane"})
		if second.Origin.WorkspaceLabel != "Caller space" || second.Origin.TabLabel != "Caller tab" || second.Origin.PaneLabel != "Caller pane" {
			t.Fatal(second.Origin)
		}
		if s.app.AgentStateChangeSeq(1) != seq {
			t.Fatal("duplicate transition")
		}
		unknown := create(&api.DecisionOrigin{PaneID: "p999"})
		if unknown.Origin.WorkspaceLabel != "" || unknown.Origin.TabLabel != "" || unknown.Origin.PaneLabel != "" {
			t.Fatal(unknown)
		}
		for _, record := range []api.Decision{d, second} {
			s.store.CancelDecision(record.DecisionID, time.Now())
		}
		s.syncDecisionAttention()
		if s.app.PaneStatusKey(1) != before {
			t.Fatal("underlying state not restored")
		}
		s.app.SetReported(1, "claude", "blocked")
		seq = s.app.AgentStateChangeSeq(1)
		d = create(&api.DecisionOrigin{PaneID: "p1"})
		if s.app.AgentStateChangeSeq(1) != seq {
			t.Fatal("already blocked transitioned")
		}
		s.app.SetReported(1, "claude", "working")
		if s.app.PaneStatusKey(1) != "blocked" {
			t.Fatal("overlay lost")
		}
		s.store.CancelDecision(d.DecisionID, time.Now())
		s.syncDecisionAttention()
		if s.app.PaneStatusKey(1) != "working" {
			t.Fatal("hook state was overwritten")
		}
		s.app.SetDecisionAttention(nil)
		d = create(&api.DecisionOrigin{PaneID: "p1"})
		s.app.SetDecisionAttention(nil)
		s.syncDecisionAttention()
		if s.app.PaneStatusKey(1) != "blocked" {
			t.Fatal("restore did not synchronize")
		}
		_ = d
	})
}
