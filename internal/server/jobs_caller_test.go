package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
	"time"
)

func TestRunCallerCompletionAdmission(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)
	caller := firstPaneID(t, dir)
	store, err := dispatch.Open(dispatch.DBPath("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, completion := range []string{"", "summary", "full", "none"} {
		pane := caller
		if completion == "none" {
			pane = "p999999"
		}
		before, err := store.MaxJobSeq()
		if err != nil {
			t.Fatal(err)
		}
		res := apiReq(t, dir, api.MethodRunStart, api.RunStartParams{CallerPane: pane, Completion: completion, Argv: []string{"/usr/bin/true"}})
		if res.Err == nil || res.Err.Code != api.ErrAgentNotFound {
			t.Fatal(completion, res)
		}
		after, err := store.MaxJobSeq()
		if err != nil || before != after {
			t.Fatal("rejection created a job", before, after, err)
		}
	}
	// A shell label alone does not create an agent identity.
	label := "ordinary-shell"
	apiOK(t, dir, api.MethodPaneRename, api.PaneRenameParams{PaneID: caller, Label: &label}, nil)
	for _, target := range []string{caller, label} {
		res := apiReq(t, dir, api.MethodRunStart, api.RunStartParams{CallerPane: target, Completion: "summary", Argv: []string{"/usr/bin/true"}})
		if res.Err == nil || res.Err.Code != api.ErrAgentNotFound {
			t.Fatal(res)
		}
	}
	var silent api.RunStartedResult
	apiOK(t, dir, api.MethodRunStart, api.RunStartParams{CallerPane: caller, Completion: "none", Argv: []string{"/usr/bin/true"}}, &silent)
	deadline := time.Now().Add(10 * time.Second)
	for jobStatus(t, store, silent.Job) != "exited" {
		if time.Now().After(deadline) {
			t.Fatal("silent job did not exit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	pending, err := store.PendingJobMessages()
	if err != nil || len(pending) != 0 {
		t.Fatal(pending, err)
	}
	reportAgent(t, dir, caller, "codex", "")
	apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: caller, Source: "test", Agent: "codex", State: "blocked"}, nil)
	for _, completion := range []string{"summary", "full"} {
		var started api.RunStartedResult
		apiOK(t, dir, api.MethodRunStart, api.RunStartParams{CallerPane: caller, Completion: completion, Argv: []string{"/usr/bin/true"}}, &started)
	}
}
