package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestWorkspaceGitRefreshSharesSameCwdAndRefreshesNextPass(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	repo := t.TempDir()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(realGit, "-C", repo, "init", "-b", "shared").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	a.st.Workspaces[0].Cwd = repo
	wrapper := t.TempDir()
	log := filepath.Join(wrapper, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\nexec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(wrapper, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "PATH", wrapper+string(os.PathListSeparator)+os.Getenv("PATH"))
	refresh := func() []byte {
		t.Helper()
		if err := os.WriteFile(log, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		a.gitAt = time.Time{}
		a.refreshWorkspaceGit(time.Now())
		if !a.gitPending {
			return nil
		}
		select {
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-time.After(5 * time.Second):
			t.Fatal("git metadata did not complete")
		}
		b, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	one := refresh()
	want := a.workspaceGit[0]
	a.st.Apply(model.CmdNewWorkspace{Cwd: repo, Name: "second"})
	two := refresh()
	if !reflect.DeepEqual(a.workspaceGit[1], want) || len(two) > 0 && strings.TrimSpace(string(one)) != strings.TrimSpace(string(two)) {
		t.Fatalf("same cwd repeated queries or changed metadata: one=%s two=%s git=%v", one, two, a.workspaceGit)
	}
	if out, err := exec.Command(realGit, "-C", repo, "symbolic-ref", "HEAD", "refs/heads/changed").CombinedOutput(); err != nil {
		t.Fatalf("change real branch: %v %s", err, out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for a.workspaceGit[0].Branch != "changed" || a.workspaceGit[1].Branch != "changed" {
		refresh()
		if time.Now().After(deadline) {
			t.Fatal("changed Git branch was not refreshed", a.workspaceGit)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if a.workspaceGit[0].Branch != "changed" || a.workspaceGit[1].Branch != "changed" {
		t.Fatal("shared metadata remained cached across refreshes", a.workspaceGit)
	}
}
