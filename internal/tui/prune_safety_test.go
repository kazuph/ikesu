package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/platform"
	"github.com/kazuph/ikesu/internal/worktree"
	"golang.org/x/sys/unix"
)

type pruneFixture struct {
	app        *App
	root, path string
	ws         *model.Workspace
	pane       int
}

func newPruneFixture(t *testing.T, prepare func(root string)) pruneFixture {
	t.Helper()
	a, root := worktreeApp(t)
	t.Cleanup(a.sup.KillAll)
	pruneTestGit(t, root, "branch", "-M", "main")
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	if prepare != nil {
		prepare(root)
	}
	path := filepath.Join(t.TempDir(), "feature")
	pruneTestGit(t, root, "worktree", "add", "-qb", "feature", path)
	opened, err := executeTestWorktree(t, a, WorktreeRequest{Operation: "open", Cwd: root, Path: &path})
	if err != nil {
		t.Fatal(err)
	}
	ws := a.st.Workspaces[opened.Outcome.Index]
	return pruneFixture{a, root, path, ws, ws.TabRef().Layout.Focused()}
}

func pruneTestRun[T any](t *testing.T, a *App, operation func() T) T {
	t.Helper()
	done := make(chan T, 1)
	go func() { done <- operation() }()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		select {
		case result := <-done:
			return result
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-timer.C:
			t.Fatal("prune did not complete")
			var zero T
			return zero
		}
	}
}

func (f pruneFixture) scan(t *testing.T) endpoint.PruneScanResult {
	t.Helper()
	return pruneTestRun(t, f.app, func() endpoint.PruneScanResult {
		return f.app.PruneScan(context.Background(), "test", noPruneViewers)
	})
}

func (f pruneFixture) candidate(t *testing.T) endpoint.PruneCandidate {
	t.Helper()
	result := f.scan(t)
	if len(result.Candidates) != 1 || result.Candidates[0].Workspace != f.ws.ID {
		t.Fatalf("prune scan: %+v", result)
	}
	return result.Candidates[0]
}

func (f pruneFixture) assertProtected(t *testing.T, candidate endpoint.PruneCandidate, scanReason, execReason string) {
	t.Helper()
	scan := f.scan(t)
	if scanReason != "" && (len(scan.Candidates) != 0 || scan.Protected[scanReason] != 1) {
		t.Fatalf("prune scan: %+v, want %q protection", scan, scanReason)
	}
	result := pruneTestRun(t, f.app, func() endpoint.PruneExecResult {
		return f.app.PruneExec(context.Background(), "test", endpoint.PruneExecRequest{
			ServerGeneration: "test", Candidates: []endpoint.PruneCandidate{candidate},
		}, noPruneViewers, func() bool { return true })
	})
	if len(result.Outcomes) != 1 || result.Outcomes[0].State != endpoint.PruneSkipped || result.Outcomes[0].Reason != execReason {
		t.Fatalf("prune exec: %+v, want %q", result, execReason)
	}
	if len(f.app.st.Workspaces) != 2 || f.app.st.Workspaces[1] != f.ws || f.app.sup.Get(f.pane) == nil {
		t.Fatal("prune closed the protected space or pane")
	}
	if _, err := os.Stat(f.path); err != nil {
		t.Fatalf("protected checkout: %v", err)
	}
}

func TestPruneProtectsLiveCwdOfOtherSpacePanes(t *testing.T) {
	f := newPruneFixture(t, func(root string) {
		dir := filepath.Join(root, "child")
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tracked"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		pruneTestGit(t, root, "add", "child")
		pruneTestCommit(t, root)
	})
	a := f.app
	rootPane := a.st.Workspaces[0].TabRef().Layout.Focused()
	if err := a.ExecCmd(model.CmdNewTab{Workspace: 0, Cwd: f.root, NoFocus: true}); err != nil {
		t.Fatal(err)
	}
	normal := a.st.Workspaces[0].Tabs[1].Layout.Focused()
	if _, err := a.OpenPluginPopup(f.root, []string{"/bin/sh", "-i"}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	popup := a.st.Popup.PaneID
	overlay, err := a.OpenOverlay(rootPane, f.root, []string{"/bin/sh", "-i"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	move := func(t *testing.T, pid int, cwd string) {
		t.Helper()
		pane := a.sup.Get(pid)
		a.sup.Input(pid, fmt.Appendf(nil, "cd '%s'\n", cwd))
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			cwds := pane.LiveCwds()
			for _, dir := range cwds {
				if dir != "" && worktree.SamePath(dir, cwd) {
					return
				}
			}
			select {
			case <-tick.C:
			case <-timer.C:
				t.Fatalf("pane %d did not reach %s, observed %v", pid, cwd, cwds)
			}
		}
	}
	for _, tc := range []struct {
		name string
		id   int
		cwd  string
	}{
		{"inactive tab", normal, filepath.Join(f.path, "child")},
		{"popup", popup, f.path},
		{"overlay", overlay, filepath.Join(f.path, "child")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pane := a.sup.Get(tc.id)
			candidate := f.candidate(t)
			move(t, tc.id, tc.cwd)
			f.assertProtected(t, candidate, "checkout used by another workspace", "checkout used by another workspace")
			if a.sup.Get(tc.id) != pane {
				t.Fatal("the pane using the checkout was replaced")
			}
			move(t, tc.id, f.root)
		})
	}
}

func TestPruneProtectsHiddenOverlayActivity(t *testing.T) {
	f := newPruneFixture(t, nil)
	a := f.app
	a.ExecCmd(model.CmdSelectWorkspace{Index: 1})
	pid, err := a.OpenOverlay(f.pane, f.path, []string{"/bin/sh", "-i"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.ExecCmd(model.CmdSelectWorkspace{Index: 0})
	if a.st.Overlay == nil || a.st.OverlayVisible() {
		t.Fatal("expected a hidden live overlay")
	}
	candidate := f.candidate(t)
	for _, status := range []string{"working", "blocked"} {
		a.SetReported(pid, "codex", status)
		f.assertProtected(t, candidate, "active agent", "active agent")
	}
	a.SetReported(pid, "codex", "idle")
	candidate = f.candidate(t)
	a.SetReported(pid, "codex", "working")
	a.SetReported(pid, "codex", "idle")
	f.assertProtected(t, candidate, "", "workspace changed")
	if a.sup.Get(pid) == nil {
		t.Fatal("prune closed the hidden overlay")
	}
}

func TestPrunePreservesTrackedFilesHiddenByIndexFlags(t *testing.T) {
	f := newPruneFixture(t, func(root string) {
		if err := os.WriteFile(filepath.Join(root, "tracked"), []byte("committed\n"), 0600); err != nil {
			t.Fatal(err)
		}
		pruneTestGit(t, root, "add", "tracked")
		pruneTestCommit(t, root)
	})
	gitDir := strings.TrimSpace(pruneTestGitOutput(t, f.path, "rev-parse", "--absolute-git-dir"))
	index := filepath.Join(gitDir, "index")
	file := filepath.Join(f.path, "tracked")
	read := func(path string) []byte {
		t.Helper()
		value, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, flag := range []string{"assume-unchanged", "skip-worktree"} {
		t.Run(flag, func(t *testing.T) {
			candidate := f.candidate(t)
			pruneTestGit(t, f.path, "update-index", "--"+flag, "tracked")
			draft := []byte("unsaved " + flag + "\n")
			if err := os.WriteFile(file, draft, 0600); err != nil {
				t.Fatal(err)
			}
			if status := pruneTestGitOutput(t, f.path, "status", "--porcelain"); status != "" {
				t.Fatalf("fixture must hide the draft from status: %q", status)
			}
			before := read(index)
			f.assertProtected(t, candidate, "index hides tracked changes", "index hides tracked changes")
			if !bytes.Equal(read(file), draft) || !bytes.Equal(read(index), before) {
				t.Fatal("prune changed the tracked draft or its index")
			}
			pruneTestGit(t, f.path, "update-index", "--no-"+flag, "tracked")
			pruneTestGit(t, f.path, "restore", "--", "tracked")
		})
	}
}

func TestPrunePreservesInitializedCleanSubmoduleAndPane(t *testing.T) {
	f := newPruneFixture(t, func(root string) {
		module := t.TempDir()
		pruneTestGit(t, module, "init", "-q")
		if err := os.WriteFile(filepath.Join(module, "data"), []byte("submodule contents\n"), 0600); err != nil {
			t.Fatal(err)
		}
		pruneTestGit(t, module, "add", "data")
		pruneTestCommit(t, module)
		pruneTestGit(t, root, "-c", "protocol.file.allow=always", "submodule", "add", "--", module, "sub")
		pruneTestCommit(t, root)
	})
	candidate := f.candidate(t)
	pruneTestGit(t, f.path, "-c", "protocol.file.allow=always", "submodule", "update", "--init")
	if status := pruneTestGitOutput(t, f.path, "status", "--porcelain", "--ignore-submodules=none"); status != "" {
		t.Fatalf("submodule must be clean: %q", status)
	}
	f.assertProtected(t, candidate, "initialized submodule", "initialized submodule")
	if data, err := os.ReadFile(filepath.Join(f.path, "sub", "data")); err != nil || string(data) != "submodule contents\n" {
		t.Fatalf("submodule data: %q, %v", data, err)
	}
}

func pruneTestCommit(t *testing.T, root string) {
	t.Helper()
	pruneTestGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "add tracked content")
}

func pruneTestGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// The two stages use a real interactive shell and a FIFO. The first stage
// cannot complete until the test closes the FIFO, keeping the job foreground.
func TestPruneProtectsForegroundPipelineMemberCwd(t *testing.T) {
	f := newPruneFixture(t, nil)
	a := f.app
	id := a.st.Workspaces[0].TabRef().Layout.Focused()
	pane := a.sup.Get(id)
	candidate := f.candidate(t)
	dir := t.TempDir()
	gate, ready := filepath.Join(dir, "hold"), filepath.Join(dir, "ready")
	if err := unix.Mkfifo(gate, 0600); err != nil {
		t.Fatal(err)
	}
	hold, err := os.OpenFile(gate, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	cmd := fmt.Sprintf("set -m; /bin/cat '%s' | /bin/sh -c 'cd \"$1\" && echo \"$$\" > \"$2\" && read -r line' sh '%s' '%s'\n", gate, f.path, ready)
	a.sup.Input(id, []byte(cmd))

	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	var member int
	for member == 0 {
		if data, err := os.ReadFile(ready); err == nil {
			member, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if member == 0 {
			select {
			case <-tick.C:
			case <-timer.C:
				t.Fatalf("rear stage did not start: %v", pane.ScreenLines())
			}
		}
	}
	group, err := unix.Getpgid(member)
	job := pane.ForegroundJob()
	if err != nil || group != job.ProcessGroupID || group == member || group == pane.ShellPID() {
		t.Fatalf("expected the rear member in the shell's foreground pipeline: member=%d group=%d foreground=%+v err=%v", member, group, job, err)
	}
	for _, pid := range []int{pane.ShellPID(), group} {
		if cwd := platform.ProcCwd(pid); cwd == "" || !worktree.SamePath(cwd, f.root) {
			t.Fatalf("shell or leader cwd %q, expected %s", cwd, f.root)
		}
	}
	if cwd := platform.ProcCwd(member); cwd == "" || !worktree.SamePath(cwd, f.path) {
		t.Fatalf("rear stage cwd %q, expected %s", cwd, f.path)
	}
	t.Log("only the rear foreground stage uses the checkout")
	f.assertProtected(t, candidate, "checkout used by another workspace", "checkout used by another workspace")
	if a.sup.Get(id) != pane {
		t.Fatal("the pane running the pipeline was replaced")
	}
}
