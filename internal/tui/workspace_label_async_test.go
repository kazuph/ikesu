package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestWorkspaceLabelDoesNotBlockInputOnGit(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(realGit, "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	a.st.Workspaces[0].Cwd = filepath.Join(repo, "sub")
	gate := t.TempDir()
	// Delay the real Git command without replacing any Git output.
	script := "#!/bin/sh\nif [ \"$3\" = rev-parse ] && [ \"$4\" = --path-format=absolute ]; then\n: > '" + gate + "/started'\nwhile [ ! -f '" + gate + "/released' ]; do /bin/sleep 0.01; done\nfi\nexec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(gate, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "PATH", gate+string(os.PathListSeparator)+os.Getenv("PATH"))
	a.refreshWorkspaceGit(time.Now())
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	release := func() { _ = os.WriteFile(filepath.Join(gate, "released"), nil, 0o600) }
	defer release()
	for {
		if _, err := os.Stat(filepath.Join(gate, "started")); err == nil {
			break
		}
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("real Git did not reach the blocking wrapper")
		}
	}
	done := make(chan bool, 1)
	go func() {
		_ = a.WorkspaceLabel(0)
		a.handleKey(uv.Key{Code: 'b', Mod: uv.ModCtrl}, []byte{2})
		done <- a.prefix
	}()
	blocked := false
	select {
	case prefix := <-done:
		if !prefix {
			t.Error("prefix input was not processed")
		}
	case <-time.After(5 * time.Second):
		blocked = true
		release()
		<-done
	}
	release()
	select {
	case job := <-a.jobs:
		job.fn()
		close(job.done)
	case <-time.After(5 * time.Second):
		t.Fatal("Git result did not return after release")
	}
	if blocked {
		t.Fatal("workspace label blocked the following input until Git was released")
	}
	if got := a.WorkspaceLabel(0); got != "repo" {
		t.Fatalf("resolved repository name = %q, want repo", got)
	}
}
