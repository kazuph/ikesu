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

func TestReview1RestoreUsesLoginPATHOnlyForAgentCommand(t *testing.T) {
	teststate.Lock(t)
	root, err := os.MkdirTemp("/tmp", "gtxr1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	root, _ = filepath.EvalSymlinks(root)
	bin := filepath.Join(root, "login-bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(root, "restored")
	agent := "#!/bin/sh\nprintf '%s|%s|%s' \"$PATH\" \"$1\" \"$2\" > '" + record + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(agent), 0700); err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(root, "loginsh")
	script := "#!/bin/sh\ncase \"$*\" in *-lic*) printf '\\000%s\\000' '" + bin + "'; exit;; esac\nexec /bin/sh \"$@\"\n"
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	teststate.Setenv(t, "PATH", "/usr/bin:/bin")
	teststate.Setenv(t, "GTX_STATE_DIR", filepath.Join(root, "state"))
	dir, done := startServerAt(t, "review1-restore")
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()
	reportAgent(t, dir, "p1", "claude", "sess-review1")
	reportAgent(t, dir, "p1", "", "")
	var res api.AgentRestoreResult
	apiOK(t, dir, api.MethodAgentRestore, api.AgentRestoreParams{}, &res)
	if len(res.Actions) != 1 || res.Actions[0].Status != "launched" {
		t.Fatal(res)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, err := os.ReadFile(record)
		if err == nil && string(b) == bin+"|--resume|sess-review1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(string(b), err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The one-command assignment must not leave login PATH in the shell.
	normal := filepath.Join(root, "ordinary-path")
	apiOK(t, dir, api.MethodPaneSendText, api.PaneSendTextParams{PaneID: "p1", Text: "printf '%s' \"$PATH\" > '" + normal + "'\r"}, nil)
	deadline = time.Now().Add(5 * time.Second)
	for {
		b, err := os.ReadFile(normal)
		if err == nil {
			if string(b) == bin {
				t.Fatal("restore changed shell PATH", string(b))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
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
			if string(b) != "/x" {
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
