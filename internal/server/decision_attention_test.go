package server

import (
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
	"time"
)

func TestDecisionOriginAndAttentionOverlay(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-single", "claude")
	s.app.Do(func() {
		st := s.app.State()
		s.app.ExecCmd(model.CmdRenameWorkspace{Index: 0, Name: "Review space"})
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

func TestDecisionRemoteOriginDoesNotNameOrBlockLocalPane(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-single", "claude")
	s.app.Do(func() {
		st := s.app.State()
		s.app.ExecCmd(model.CmdRenameWorkspace{Index: 0, Name: "Local space"})
		st.Workspaces[0].Tabs[0].Name = "Local tab"
		st.PaneLabels = map[int]string{1: "Local worker"}
		s.app.SetReported(1, "claude", "working")
		for _, machine := range []string{"mini", "", "local"} {
			resp := s.decisionRequest(&api.Request{ID: "machine-origin", Method: api.MethodDecisionCreate, Params: api.MarshalParams(api.DecisionCreateParams{Kind: "ask", Title: "Review?", Origin: &api.DecisionOrigin{PaneID: "p1", Machine: machine}, Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}})})
			if resp.Err != nil {
				t.Error(resp.Err)
				return
			}
			var result api.DecisionResult
			if err := json.Unmarshal(resp.Result, &result); err != nil {
				t.Error(err)
				return
			}
			origin := result.Decision.Origin
			if origin.Machine != machine {
				t.Errorf("machine was changed: %+v", origin)
			}
			if machine == "mini" {
				if origin.WorkspaceLabel != "" || origin.TabLabel != "" || origin.PaneLabel != "" {
					t.Errorf("remote origin received local names: %+v", origin)
				}
				if got := s.app.PaneStatusKey(1); got != "working" {
					t.Errorf("remote p1 changed local p1 state: %s", got)
				}
				// Repeat startup/handoff synchronization with the remote record still pending.
				s.app.SetDecisionAttention(nil)
				s.syncDecisionAttention()
				if got := s.app.PaneStatusKey(1); got != "working" {
					t.Errorf("restored remote record blocked local p1: %s", got)
				}
			} else {
				if origin.WorkspaceLabel != "Local space" || origin.TabLabel != "Local tab" || origin.PaneLabel != "Local worker" {
					t.Errorf("local origin names missing for machine %q: %+v", machine, origin)
				}
				if got := s.app.PaneStatusKey(1); got != "blocked" {
					t.Errorf("local origin did not block p1 for machine %q: %s", machine, got)
				}
			}
			cancelled := s.decisionRequest(&api.Request{ID: "cancel-machine-origin", Method: api.MethodDecisionCancel, Params: api.MarshalParams(api.DecisionTargetParams{DecisionID: result.Decision.DecisionID})})
			if cancelled.Err != nil {
				t.Error(cancelled.Err)
				return
			}
			if got := s.app.PaneStatusKey(1); got != "working" {
				t.Errorf("cancellation did not restore local state: %s", got)
			}
		}
	})
}
