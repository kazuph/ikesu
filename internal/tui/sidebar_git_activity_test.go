package tui

import (
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceGitQuietDoesNotStartAnotherQuery(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "quiet")
	a.st.Workspaces[0].Cwd = repo
	a.refreshWorkspaceGit(time.Now())
	finish := func() {
		t.Helper()
		select {
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-time.After(5 * time.Second):
			t.Fatal("initial Git refresh did not finish")
		}
	}
	finish()
	a.refreshWorkspaceGit(a.gitAt.Add(1500 * time.Millisecond))
	if a.gitPending {
		finish()
		t.Fatal("unchanged workspace started another Git query")
	}
}

func waitWorkspaceGitActivity(t *testing.T, a *App, check func() bool) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for {
		a.refreshWorkspaceGit(time.Now())
		if a.gitPending {
			select {
			case job := <-a.jobs:
				job.fn()
				close(job.done)
			case <-time.After(5 * time.Second):
				t.Fatal("Git activity query did not finish")
			}
		}
		if check() {
			return
		}
		if time.Now().After(until) {
			t.Fatal("Git activity did not preserve the existing refresh deadline", a.workspaceGit, a.workspaceNames)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWorkspaceGitActivityPreservesTrackedChangesAndLinkedHead(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "source")
	linked := filepath.Join(dir, "linked")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	nested := filepath.Join(repo, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(nested, "tracked")
	if err := os.WriteFile(tracked, []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "add", ".")
	sidebarGit(t, repo, "commit", "-qm", "base")
	sidebarGit(t, repo, "worktree", "add", "-qb", "linked", linked)
	a.st.Workspaces[0].Cwd = repo
	a.st.Apply(model.CmdNewWorkspace{Cwd: linked, Name: "linked"})
	waitWorkspaceGitActivity(t, a, func() bool {
		return a.workspaceGit[0].Branch == "main" && a.workspaceGit[1].Branch == "linked" && a.workspaceNames[1] == "linked"
	})
	if err := os.WriteFile(tracked, []byte("changed\none\ntwo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 3 && a.workspaceGit[0].Deleted == 1 })
	replacement := tracked + ".new"
	if err := os.WriteFile(replacement, []byte("replaced\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, tracked); err != nil {
		t.Fatal(err)
	}
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 1 && a.workspaceGit[0].Deleted == 1 })
	sidebarGit(t, repo, "branch", "-m", "renamed")
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Branch == "renamed" && a.workspaceGit[1].Branch == "linked" })
	sidebarGit(t, linked, "symbolic-ref", "HEAD", "refs/heads/linked-new")
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[1].Branch == "linked-new" && a.workspaceNames[1] == "linked" })
}

func TestWorkspaceGitActivityFindsInitAndCwdChange(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	dir := t.TempDir()
	a.st.Workspaces[0].Cwd = dir
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceLabels[a.st.Workspaces[0].ID].cwd == dir })
	if a.workspaceGit[0].Branch != "" {
		t.Fatal("nonrepository acquired a Git branch")
	}
	sidebarGit(t, dir, "init", "-qb", "created")
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Branch == "created" })
	other := t.TempDir()
	sidebarGit(t, other, "init", "-qb", "other")
	a.st.Workspaces[0].Cwd = other
	waitWorkspaceGitActivity(t, a, func() bool {
		return a.workspaceGit[0].Branch == "other" && a.workspaceLabels[a.st.Workspaces[0].ID].cwd == other
	})
}

func TestWorkspaceGitActivityTracksAttributesOutsideIndex(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	tracked := filepath.Join(repo, "tracked")
	if err := os.WriteFile(tracked, []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "add", "tracked")
	sidebarGit(t, repo, "commit", "-qm", "base")
	if err := os.WriteFile(tracked, []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.st.Workspaces[0].Cwd = repo
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 1 })
	attrs := filepath.Join(repo, ".gitattributes")
	if err := os.WriteFile(attrs, []byte("tracked -diff\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 0 })
	if err := os.WriteFile(attrs, []byte("tracked diff\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 1 })
	if err := os.Remove(attrs); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "attributes")
	if err := os.WriteFile(outside, []byte("tracked -diff\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "config", "core.attributesfile", outside)
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 0 })
	if err := os.WriteFile(outside, []byte("tracked diff\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 1 })
}
