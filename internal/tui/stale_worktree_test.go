package tui

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"github.com/kazuph/ikesu/internal/worktree"
)

func TestWorktreeDeleteDefersReanchorUntilGitFinishes(t *testing.T) {
	teststate.Lock(t)
	testWorktreeDeleteDefersReanchor(t, false)
}

func TestViewerWorktreeDeleteDefersReanchorUntilGitFinishes(t *testing.T) {
	teststate.Lock(t)
	testWorktreeDeleteDefersReanchor(t, true)
}

func testWorktreeDeleteDefersReanchor(t *testing.T, viewer bool) {
	t.Helper()
	a, repo := worktreeApp(t)
	var existingViewer *Viewer
	if viewer {
		existingViewer = a.NewViewer(io.Discard, a.st.Cols, a.st.Rows, nil)
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked"), []byte("data\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "add", "tracked")
	sidebarGit(t, repo, "-c", "user.name=Test", "-c", "user.email=t@t", "commit", "-qm", "tracked")
	branch := "delete-during-git"
	created, err := executeTestWorktree(t, a, WorktreeRequest{Operation: "create", Branch: &branch})
	if err != nil {
		t.Fatal(err)
	}
	index, path := created.Outcome.Index, created.Outcome.Worktree.Path
	root := t.TempDir()
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, destination, "init", "-q")
	destinationSource, err := worktree.Source(destination)
	if err != nil {
		t.Fatal(err)
	}
	fifo, ready := filepath.Join(root, "gate"), filepath.Join(root, "ready")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	gate, err := os.OpenFile(fifo, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	defer func() { _, _ = gate.WriteString("go\n") }()
	hook := filepath.Join(root, "fsmonitor")
	released := filepath.Join(root, "released")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nif [ ! -f '"+released+"' ]; then\nprintf ready > '"+ready+"'\nread x < '"+fifo+"'\n: > '"+released+"'\nfi\nprintf '\\0'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "config", "core.fsmonitor", hook)
	req, ok := a.workspaceWorktreeRequest(index, "remove")
	if !ok {
		t.Fatal("workspace missing")
	}
	result := make(chan error, 1)
	executor := a
	if viewer {
		existingViewer.with(a, func(ui *App) { executor = ui })
	}
	go func() { _, err := executor.ExecuteWorktree(req); result <- err }()
	deadline := time.After(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("Git did not enter fsmonitor")
		}
	}
	w := a.st.Workspaces[index]
	pane := a.sup.Get(w.Tabs[0].Layout.PaneIDs()[0])
	if _, err := pane.WriteToPTY([]byte("cd '" + destination + "'; printf 'MOVE_%s\\n' READY\n")); err != nil {
		t.Fatal(err)
	}
	specWaitText(t, pane, "MOVE_READY")
	target := a.captureWorkspaceGitTarget(w)
	a.refreshStaleWorktrees([]workspaceGitTarget{target}, map[string]gitCwdActivity{target.nameCwd: {loaded: true, source: destinationSource}})
	movedDuringDelete := w.Cwd != path
	_, _ = gate.WriteString("go\n")
	deadline = time.After(3 * time.Second)
	for {
		select {
		case err := <-result:
			if movedDuringDelete {
				t.Fatal("workspace was re-anchored while checkout deletion was running")
			}
			if err != nil {
				t.Fatalf("removing the confirmed checkout failed: %v", err)
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("confirmed checkout was not removed: %v", statErr)
			}
			return
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-deadline:
			t.Fatal("Git removal did not finish")
		}
	}
}

func TestWorktreeCreateDefersReanchorUntilGitFinishes(t *testing.T) {
	teststate.Lock(t)
	for _, explicit := range []bool{false, true} {
		name := "workspace_id"
		if explicit {
			name = "explicit_cwd"
		}
		t.Run(name, func(t *testing.T) { testWorktreeCreateDefersReanchor(t, explicit, false) })
	}
}

func TestViewerWorktreeCreateDefersReanchorUntilGitFinishes(t *testing.T) {
	teststate.Lock(t)
	testWorktreeCreateDefersReanchor(t, false, true)
}

func testWorktreeCreateDefersReanchor(t *testing.T, explicit, viewer bool) {
	t.Helper()
	a, repo := worktreeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	w := a.st.Workspaces[0]
	original := w.Cwd
	originalSource, err := worktree.Source(repo)
	if err != nil {
		t.Fatal(err)
	}
	w.Worktree = worktree.Metadata(originalSource, originalSource.SourceCheckoutPath)
	root := t.TempDir()
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, destination, "init", "-q")
	destinationSource, err := worktree.Source(destination)
	if err != nil {
		t.Fatal(err)
	}
	fifo, ready := filepath.Join(root, "gate"), filepath.Join(root, "ready")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	gate, err := os.OpenFile(fifo, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	defer func() { _, _ = gate.WriteString("go\n") }()
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf ready > '"+ready+"'\nread x < '"+fifo+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	req, ok := a.workspaceWorktreeRequest(0, "create")
	if !ok {
		t.Fatal("workspace missing")
	}
	if explicit {
		req.WorkspaceID = nil
		req.Cwd = original
		req.ExpectedGeneration, req.ExpectedCwd = "", ""
	}
	branch := "create-during-move"
	req.Branch = &branch
	result := make(chan error, 1)
	executor := a
	if viewer {
		v := a.NewViewer(io.Discard, a.st.Cols, a.st.Rows, nil)
		v.with(a, func(ui *App) { executor = ui })
	}
	go func() { _, err := executor.ExecuteWorktree(req); result <- err }()
	deadline := time.After(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("post-checkout hook did not start")
		}
	}
	pane := a.sup.Get(w.Tabs[0].Layout.PaneIDs()[0])
	if _, err := pane.WriteToPTY([]byte("cd '" + destination + "'; printf 'MOVE_%s\\n' READY\n")); err != nil {
		t.Fatal(err)
	}
	specWaitText(t, pane, "MOVE_READY")
	target := a.captureWorkspaceGitTarget(w)
	entries := map[string]gitCwdActivity{target.nameCwd: {loaded: true, source: destinationSource}}
	a.refreshStaleWorktrees([]workspaceGitTarget{target}, entries)
	movedDuringCreate := w.Cwd != original
	_, _ = gate.WriteString("go\n")
	deadline = time.After(3 * time.Second)
	for {
		select {
		case err := <-result:
			if movedDuringCreate {
				t.Fatal("workspace was re-anchored while checkout creation was running")
			}
			if err != nil {
				t.Fatal(err)
			}
			target = a.captureWorkspaceGitTarget(w)
			a.refreshStaleWorktrees([]workspaceGitTarget{target}, entries)
			if w.Cwd != target.nameCwd {
				t.Fatalf("workspace did not re-anchor after creation: %q", w.Cwd)
			}
			return
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-deadline:
			t.Fatal("worktree creation did not finish")
		}
	}
}

func TestWorktreeDeleteRechecksWorkspaceAfterGitLookup(t *testing.T) {
	teststate.Lock(t)
	a, _ := worktreeApp(t)
	branch := "delete-check"
	created, err := executeTestWorktree(t, a, WorktreeRequest{Operation: "create", Branch: &branch})
	if err != nil {
		t.Fatal(err)
	}
	index, path := created.Outcome.Index, created.Outcome.Worktree.Path
	req, ok := a.workspaceWorktreeRequest(index, "remove")
	if !ok {
		t.Fatal("workspace missing")
	}
	result := make(chan error, 1)
	go func() { _, err := a.ExecuteWorktree(req); result <- err }()
	readJob := func() appJob {
		select {
		case job := <-a.jobs:
			return job
		case <-time.After(2 * time.Second):
			t.Fatal("Git phase did not reach app loop")
			return appJob{}
		}
	}
	first := readJob()
	first.fn()
	close(first.done)
	beforeDelete := readJob()
	a.st.Workspaces[index].Cwd = t.TempDir()
	beforeDelete.fn()
	close(beforeDelete.done)
	until := time.After(2 * time.Second)
	for {
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("stale deletion was accepted")
			}
			goto finished
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-until:
			t.Fatal("stale deletion did not return")
		}
	}
finished:
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("checkout was removed after workspace moved: %v", err)
	}
}

func TestStaleGitResultDoesNotRestorePreviousCheckout(t *testing.T) {
	teststate.Lock(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldDir, newDir := filepath.Join(root, "old"), filepath.Join(root, "new")
	for _, dir := range []string{oldDir, newDir} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		sidebarGit(t, dir, "init", "-q")
	}
	oldSub := filepath.Join(oldDir, "sub")
	if err := os.Mkdir(oldSub, 0700); err != nil {
		t.Fatal(err)
	}
	oldSource, err := worktree.Source(oldDir)
	if err != nil {
		t.Fatal(err)
	}
	newSource, err := worktree.Source(newDir)
	if err != nil {
		t.Fatal(err)
	}
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	p := a.sup.Get(a.focusedPane())
	if _, err := p.WriteToPTY([]byte("cd '" + oldSub + "'; printf 'OLD_%s\\n' READY\n")); err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "OLD_READY")
	w := a.st.Workspaces[0]
	w.Cwd = oldDir
	target := a.captureWorkspaceGitTarget(w)
	if target.nameCwd != oldSub {
		t.Fatalf("first shell cwd=%q, want %q", target.nameCwd, oldSub)
	}
	if _, err := p.WriteToPTY([]byte("cd '" + newDir + "'; printf 'NEW_%s\\n' READY\n")); err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "NEW_READY")
	w.Cwd = newDir
	w.Worktree = worktree.Metadata(newSource, newDir)
	a.refreshStaleWorktrees([]workspaceGitTarget{target}, map[string]gitCwdActivity{target.nameCwd: {loaded: true, source: oldSource}})
	if w.Cwd != newDir || w.Worktree.RepoKey != newSource.RepoKey {
		t.Fatalf("old result replaced the new checkout: cwd=%q metadata=%+v", w.Cwd, w.Worktree)
	}
}

// A reused space keeps the worktree metadata of where it was created; once its
// first pane's shell lives in another checkout the space must follow it.
func TestStaleWorktreeMetadataFollowsTheFirstPaneShell(t *testing.T) {
	teststate.Lock(t)
	root := t.TempDir()
	old := &api.WorkspaceWorktreeInfo{RepoKey: filepath.Join(root, "a/.git"), RepoName: "a", RepoRoot: filepath.Join(root, "a"), CheckoutPath: filepath.Join(root, "a/.worktree/fix"), IsLinkedWorktree: true}
	other := api.WorktreeSourceInfo{RepoKey: filepath.Join(root, "b/.git"), RepoName: "b", RepoRoot: filepath.Join(root, "b"), SourceCheckoutPath: filepath.Join(root, "b")}
	for _, c := range []struct {
		name    string
		nameCwd string
		entry   gitCwdActivity
		want    *api.WorkspaceWorktreeInfo
	}{
		{"moved to another repo", filepath.Join(root, "b/src"), gitCwdActivity{loaded: true, source: other}, &api.WorkspaceWorktreeInfo{RepoKey: other.RepoKey, RepoName: "b", RepoRoot: other.RepoRoot, CheckoutPath: other.RepoRoot}},
		{"still inside the checkout", filepath.Join(root, "a/.worktree/fix/sub"), gitCwdActivity{loaded: true, source: other}, old},
		{"left git", filepath.Join(root, "plain"), gitCwdActivity{loaded: true, sourceErr: errors.New("not a git repository")}, nil},
		{"not read yet", filepath.Join(root, "b"), gitCwdActivity{source: other}, old},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := specRuntimeApp(t)
			copied := *old
			a.st.Workspaces[0].Worktree = &copied
			a.st.Workspaces[0].Cwd = old.CheckoutPath
			target := a.captureWorkspaceGitTarget(a.st.Workspaces[0])
			target.nameCwd = c.nameCwd
			a.refreshStaleWorktrees([]workspaceGitTarget{target}, map[string]gitCwdActivity{c.nameCwd: c.entry})
			got := a.st.Workspaces[0].Worktree
			if (got == nil) != (c.want == nil) || got != nil && *got != *c.want {
				t.Fatalf("worktree=%+v want %+v", got, c.want)
			}
			moved := got != old && (got == nil || *got != *old)
			if wantCwd := map[bool]string{true: c.nameCwd, false: a.st.Workspaces[0].Cwd}[moved]; a.st.Workspaces[0].Cwd != wantCwd {
				t.Fatalf("cwd=%q want %q", a.st.Workspaces[0].Cwd, wantCwd)
			}
		})
	}
}

// The git line reads the space's directory, so it follows the re-anchor, and
// coming back to the first checkout re-anchors again.
func TestStaleWorktreeRoundTripMovesTheSpaceDirectory(t *testing.T) {
	teststate.Lock(t)
	root := t.TempDir()
	a := specRuntimeApp(t)
	srcA := api.WorktreeSourceInfo{RepoKey: filepath.Join(root, "a/.git"), RepoName: "a", RepoRoot: filepath.Join(root, "a"), SourceCheckoutPath: filepath.Join(root, "a/.worktree/fix")}
	srcB := api.WorktreeSourceInfo{RepoKey: filepath.Join(root, "b/.git"), RepoName: "b", RepoRoot: filepath.Join(root, "b"), SourceCheckoutPath: filepath.Join(root, "b")}
	a.st.Workspaces[0].Cwd = srcA.SourceCheckoutPath
	a.st.Workspaces[0].Worktree = worktree.Metadata(srcA, srcA.SourceCheckoutPath)
	for _, step := range []api.WorktreeSourceInfo{srcB, srcA} {
		target := a.captureWorkspaceGitTarget(a.st.Workspaces[0])
		target.nameCwd = step.SourceCheckoutPath
		a.refreshStaleWorktrees([]workspaceGitTarget{target}, map[string]gitCwdActivity{step.SourceCheckoutPath: {loaded: true, source: step}})
		ws := a.st.Workspaces[0]
		if ws.Cwd != step.SourceCheckoutPath || ws.Worktree == nil || ws.Worktree.RepoKey != step.RepoKey || ws.Worktree.CheckoutPath != step.SourceCheckoutPath {
			t.Fatalf("after moving to %s: cwd=%q worktree=%+v", step.RepoName, ws.Cwd, ws.Worktree)
		}
	}
}

// A delete confirmation removes the checkout it showed. If the space was
// re-anchored to another checkout meanwhile, nothing is deleted.
func TestWorktreeDeleteRefusesAfterTheSpaceMoved(t *testing.T) {
	teststate.Lock(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	git := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(repo, "init", "-q")
	git(repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	a1, b1 := filepath.Join(root, "wt-a"), filepath.Join(root, "wt-b")
	git(repo, "worktree", "add", "-q", "-b", "a", a1)
	git(repo, "worktree", "add", "-q", "-b", "b", b1)
	app := specRuntimeApp(t)
	srcA, err := worktree.Source(a1)
	if err != nil {
		t.Fatal(err)
	}
	srcB, err := worktree.Source(b1)
	if err != nil {
		t.Fatal(err)
	}
	app.exec(model.CmdSetWorktree{Index: 0, Info: worktree.Metadata(srcA, a1), Cwd: a1})
	app.deleteWorktreeDialog(0, false)
	app.exec(model.CmdSetWorktree{Index: 0, Info: worktree.Metadata(srcB, b1), Cwd: b1})
	app.dialog.run(app)
	for _, dir := range []string{a1, b1} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%s must remain after a stale confirmation: %v", dir, err)
		}
	}
	app.dialog = nil
	app.deleteWorktreeDialog(0, false)
	app.dialog.run(app)
	waitWorktreeUI(t, app, func() bool { _, err := os.Stat(b1); return os.IsNotExist(err) })
	if _, err := os.Stat(b1); !os.IsNotExist(err) {
		t.Fatalf("the checkout the dialog showed must be removed: %v", err)
	}
	if _, err := os.Stat(a1); err != nil {
		t.Fatalf("the other checkout must remain: %v", err)
	}
}
