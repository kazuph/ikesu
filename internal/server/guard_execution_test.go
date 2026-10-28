package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"os/exec"
	"testing"
)

func TestGuardExecutionBindingCleanup(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "guard-cleanup")
	defer s.sup.KillAll()
	s.initDecisionHub()
	_, identity, err := guard.ApprovalProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	live := guardExecutionBinding{Owner: os.Getpid(), ProcessStart: identity}
	for _, status := range []string{"pending", "approve", "day_allow", "reject", "cancelled", "expired"} {
		s.decisions.guardBindings = map[string]guardExecutionBinding{"test": live}
		d := api.Decision{DecisionID: "test", Status: status}
		if status == "approve" || status == "day_allow" || status == "reject" {
			answer := status
			d.Status = "answered"
			d.Answer = &api.DecisionAnswer{OptionID: &answer}
		}
		s.decisionChanged("decision_resolved", d)
		want := status == "pending" || status == "approve" || status == "day_allow"
		if (len(s.decisions.guardBindings) == 1) != want {
			t.Fatalf("status=%s bindings=%d", status, len(s.decisions.guardBindings))
		}
	}
	cmd := exec.Command("/usr/bin/true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	s.decisions.guardBindings = map[string]guardExecutionBinding{"live": live, "ended": {Owner: cmd.Process.Pid}, "reused": {Owner: os.Getpid(), ProcessStart: "wrong-start"}}
	s.pruneGuardExecutions()
	if len(s.decisions.guardBindings) != 1 || s.decisions.guardBindings["live"] != live {
		t.Fatal("owner cleanup", s.decisions.guardBindings)
	}
}
