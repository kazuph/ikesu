package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestReview1SlowLoginShellDoesNotBlockPaneList(t *testing.T) {
	teststate.Lock(t)
	root, err := os.MkdirTemp("/tmp", "ikesur1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	shell, loginStarted, loginRelease := teststate.GatedLoginShell(t)
	teststate.Setenv(t, "SHELL", shell)
	teststate.Setenv(t, "IKESU_STATE_DIR", filepath.Join(root, "state"))
	dir, done := startServerAt(t, "review1-slow")
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); waitServerExit(t, done) }()
	started := make(chan api.Response, 1)
	go func() {
		started <- apiReq(t, dir, api.MethodAgentStart, api.AgentStartParams{Argv: []string{"/bin/sleep", "60"}})
	}()
	waitPromptCondition(t, func() bool { b, _ := os.ReadFile(loginStarted); return string(b) == "waiting" })
	apiOK(t, dir, api.MethodPaneList, api.PaneListParams{}, nil)
	select {
	case resp := <-started:
		t.Fatal("agent launch finished before login PATH was released", resp)
	default:
	}
	if err := os.WriteFile(loginRelease, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	resp := <-started
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
}
