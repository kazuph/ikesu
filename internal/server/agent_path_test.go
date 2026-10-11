package server

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReview1SlowLoginShellDoesNotBlockPaneList(t *testing.T) {
	teststate.Lock(t)
	root, err := os.MkdirTemp("/tmp", "gtxr1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	shell := filepath.Join(root, "slowsh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\ncase \"$*\" in *-lic*) /bin/sleep 4;; esac\nexec /bin/sh \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	teststate.Setenv(t, "GTX_STATE_DIR", filepath.Join(root, "state"))
	dir, done := startServerAt(t, "review1-slow")
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()
	started := make(chan api.Response, 1)
	before := time.Now()
	go func() {
		started <- apiReq(t, dir, api.MethodAgentStart, api.AgentStartParams{Argv: []string{"/bin/sleep", "60"}})
	}()
	time.Sleep(300 * time.Millisecond)
	tick := time.Now()
	apiOK(t, dir, api.MethodPaneList, api.PaneListParams{}, nil)
	elapsed := time.Since(tick)
	t.Logf("pane list %.6f seconds during 4-second login shell", elapsed.Seconds())
	if elapsed >= 200*time.Millisecond {
		t.Fatal("app loop stalled", elapsed)
	}
	resp := <-started
	if resp.Err != nil || time.Since(before) < 3*time.Second {
		t.Fatal(resp.Err, time.Since(before))
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
	deadline := time.Now().Add(time.Second)
	for {
		b, err := os.ReadFile(record)
		if err == nil {
			if string(b) != filepath.Join(root, "state", "guard", "bin", "p_2")+":/x" {
				t.Fatal("explicit PATH overwritten", string(b))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
}
