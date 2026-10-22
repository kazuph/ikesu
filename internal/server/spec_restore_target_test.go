package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"reflect"
	"testing"
)

func TestSPECStableAgentTargetAfterRestart(t *testing.T) {
	teststate.Lock(t)
	base, err := os.MkdirTemp("/tmp", "ikesu-spec-restore-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "IKESU_STATE_DIR", base)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	dir, done := startServerAt(t, "stable")
	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "right", Focus: true}, nil)
	ids := paneIDs(t, dir)
	name := "stable-target"
	reportAgent(t, dir, ids[1], "codex", "safe-session")
	apiOK(t, dir, api.MethodAgentRename, api.AgentRenameParams{Target: ids[1], Name: &name}, nil)
	check := func(dir string) {
		var out api.AgentInfoResult
		apiOK(t, dir, api.MethodAgentFocus, api.AgentTarget{Target: name}, &out)
		if out.Agent.PaneID != ids[1] {
			t.Fatal("agent command remapped", out.Agent.PaneID, ids)
		}
		var pane api.PaneInfoResult
		apiOK(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: ids[1]}, &pane)
		if !pane.Pane.Focused {
			t.Fatal("agent focus targeted another pane")
		}
	}
	check(dir)
	waitForSessionFile(t, dir)
	apiOK(t, dir, api.MethodServerRestart, api.EmptyParams{}, nil)
	waitAPISocketGone(t, dir)
	<-done
	dir, done = startServerAt(t, "stable")
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()
	if got := paneIDs(t, dir); !reflect.DeepEqual(got, ids) {
		t.Fatal("restored IDs", got, ids)
	}
	check(dir)
}
