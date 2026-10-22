package server

import (
	"fmt"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestNativeAgentsRestoreExactPaneIDsAfterRealServerRestart(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	for i := 0; i < len(nativeResumeAgents); i++ {
		apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "right"}, nil)
	}
	ids := paneIDs(t, dir)
	agents := []string{"grok", "agy", "qwen", "letta"}
	want := map[string]string{}
	for i, agent := range agents {
		id := fmt.Sprintf("conversation-%d", i+1)
		reportAgent(t, dir, ids[i], agent, id)
		want[ids[i]] = agent + " " + nativeResumeAgents[agent].Flag + " " + id
	}
	// A detected agent without a recorded conversation must stay a shell.
	reportAgent(t, dir, ids[len(agents)], "letta", "")
	store, err := dispatch.OpenReadOnly(dispatch.DBPath("test"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.PaneSessions("test")
	store.Close()
	if err != nil || len(rows) != len(agents) {
		t.Fatalf("exact per-pane ledger: %+v %v", rows, err)
	}
	waitForSessionFile(t, dir)
	apiOK(t, dir, api.MethodServerRestart, api.EmptyParams{}, nil)
	waitAPISocketGone(t, dir)
	waitServerExit(t, done)
	dir, done = startServerAt(t, "test")
	defer stopServer(t, dir, done)
	var result api.AgentRestoreResult
	apiOK(t, dir, api.MethodAgentRestore, api.AgentRestoreParams{DryRun: true}, &result)
	for _, action := range result.Actions {
		command, saved := want[action.PaneID]
		if !saved {
			if action.Command != nil || action.Status == "would_launch" {
				t.Fatalf("pane without a saved session would launch: %+v", action)
			}
			continue
		}
		if action.Status != "would_launch" || action.Command == nil || *action.Command != command {
			t.Fatalf("pane %s expected %q: %+v", action.PaneID, command, action)
		}
		delete(want, action.PaneID)
	}
	if len(want) != 0 {
		t.Fatalf("missing exact restore plans: %v", want)
	}
}
