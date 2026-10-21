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
	root, err := os.MkdirTemp("/tmp", "gtxr1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	shell, loginStarted, loginRelease := teststate.GatedLoginShell(t)
	teststate.Setenv(t, "SHELL", shell)
	teststate.Setenv(t, "GTX_STATE_DIR", filepath.Join(root, "state"))
	dir, done := startServerAt(t, "review1-slow")
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()
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

func TestReview1AgentStartExplicitPATH(t *testing.T) {
	teststate.Lock(t)
	root, err := os.MkdirTemp("/tmp", "gtxr1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	shell := filepath.Join(root, "loginsh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\ncase \"$*\" in *-lic*) printf '\\000/usr/bin:/bin\\000'; exit;; esac\nexec /bin/sh \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	teststate.Setenv(t, "GTX_STATE_DIR", filepath.Join(root, "state"))
	dir, done := startServerAt(t, "explicit")
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()
	record := filepath.Join(root, "env")
	var res api.AgentStartedResult
	apiOK(t, dir, api.MethodAgentStart, api.AgentStartParams{Env: map[string]string{"PATH": "/x"}, Argv: []string{"/bin/sh", "-c", "printf '%s' \"$PATH\" > '" + record + "'; exec /bin/sleep 60"}}, &res)
	var observed []byte
	waitPromptCondition(t, func() bool { observed, _ = os.ReadFile(record); return len(observed) > 0 })
	if string(observed) != filepath.Join(root, "state", "guard", "bin", "p_2")+":/x" {
		t.Fatal("explicit PATH overwritten", string(observed))
	}
}
