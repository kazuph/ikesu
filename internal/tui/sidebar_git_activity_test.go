package tui

import (
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWorkspaceGitSlowRepositoryDoesNotBlockOtherUpdates(t *testing.T) {
	teststate.Lock(t)
	root := t.TempDir()
	slow, fast := filepath.Join(root, "slow"), filepath.Join(root, "fast")
	for _, dir := range []string{slow, fast} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		sidebarGit(t, dir, "init", "-qb", "initial")
		if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte("one\n"), 0600); err != nil {
			t.Fatal(err)
		}
		sidebarGit(t, dir, "add", "tracked")
		sidebarGit(t, dir, "-c", "user.name=Test", "-c", "user.email=t@t", "commit", "-qm", "initial")
	}
	fifo, ready := filepath.Join(root, "fifo"), filepath.Join(root, "ready")
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
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf '%s' $$ > '"+ready+"'\nread x < '"+fifo+"'\nprintf '\\0'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, slow, "config", "core.fsmonitor", hook)
	a := specRuntimeApp(t)
	a.st.Workspaces[0].Cwd = slow
	a.st.Apply(model.CmdNewWorkspace{Cwd: fast, Name: "fast"})
	a.refreshWorkspaceGit(time.Now())
	deadline := time.After(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil && a.workspaceGit[1].Branch == "initial" {
			break
		}
		select {
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("slow Git blocked the other workspace's initial result")
		}
	}
	sidebarGit(t, fast, "symbolic-ref", "HEAD", "refs/heads/updated")
	a.refreshWorkspaceGit(a.gitAt.Add(1500 * time.Millisecond))
	deadline = time.After(3 * time.Second)
	for a.workspaceGit[1].Branch != "updated" {
		select {
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-deadline:
			t.Fatal("slow Git blocked the other workspace's subsequent update")
		}
	}
	pidText, err := os.ReadFile(ready)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidText)))
	if err != nil {
		t.Fatal(err)
	}
	a.st.Apply(model.CmdCloseWorkspace{Index: 0})
	a.refreshWorkspaceGit(a.gitAt.Add(1500 * time.Millisecond))
	deadline = time.After(3 * time.Second)
	for a.gitActivity.running[slow] != nil {
		select {
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-deadline:
			t.Fatal("closing the workspace did not cancel its Git process")
		}
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatal("cancelled Git helper is still running")
	}
}

func trackGitCalls(t *testing.T) string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\nexec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}
func finishGitRefresh(t *testing.T, a *App) {
	t.Helper()
	if !a.gitPending {
		return
	}
	select {
	case job := <-a.jobs:
		job.fn()
		close(job.done)
	case <-time.After(5 * time.Second):
		t.Fatal("Git refresh did not finish")
	}
}
func assertNoGitCalls(t *testing.T, log string) {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(b) > 0 {
		t.Fatalf("unchanged workspace ran git: %s", b)
	}
}

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
	log := trackGitCalls(t)
	a.refreshWorkspaceGit(a.gitAt.Add(1500 * time.Millisecond))
	finishGitRefresh(t, a)
	assertNoGitCalls(t, log)
}

func TestWorkspaceGitQuietIgnoresUnrelatedSiblingActivity(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "init", "-qb", "quiet")
	a.st.Workspaces[0].Cwd = repo
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Branch == "quiet" })
	entry := a.gitActivity.entries[repo]
	if !entry.ready {
		t.Fatal("native Git watch not established")
	}
	before := entry.watch.Revision()
	if err := os.Mkdir(filepath.Join(parent, "unrelated"), 0700); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(5 * time.Second)
	for entry.watch.Revision() == before {
		if time.Now().After(until) {
			t.Fatal("real sibling change did not reach ancestor watch")
		}
		time.Sleep(time.Millisecond)
	}
	log := trackGitCalls(t)
	a.refreshWorkspaceGit(a.gitAt.Add(1500 * time.Millisecond))
	finishGitRefresh(t, a)
	assertNoGitCalls(t, log)
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

// Multiple real Git changes must each pass the existing 1.5-second refresh cadence.
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

func TestWorkspaceGitActivityKeepsWriteWithRestoredMtime(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	path := filepath.Join(repo, "tracked")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "add", ".")
	sidebarGit(t, repo, "commit", "-qm", "base")
	a.st.Workspaces[0].Cwd = repo
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Branch == "main" })
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("xxxx"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 1 && a.workspaceGit[0].Deleted == 2 })
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

// Each real attributes change must pass the existing 1.5-second refresh cadence.
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

func TestWorkspaceGitSubdirectoryTracksFileAndAttributes(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	repo := t.TempDir()
	nested := filepath.Join(repo, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	file := filepath.Join(nested, "tracked.txt")
	if err := os.WriteFile(file, []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "add", ".")
	sidebarGit(t, repo, "commit", "-qm", "base")
	a.st.Workspaces[0].Cwd = nested
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Branch == "main" })
	if err := os.WriteFile(file, []byte("changed\none\ntwo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 3 && a.workspaceGit[0].Deleted == 1 })
	if err := os.WriteFile(filepath.Join(nested, ".gitattributes"), []byte("*.txt binary\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 0 && a.workspaceGit[0].Deleted == 0 })
}

// Three real Git changes each wait for the 1.5-second sidebar refresh.
func TestWorkspaceGitCommitAndCheckoutRefreshPromptly(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	file := filepath.Join(repo, "tracked")
	if err := os.WriteFile(file, []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "add", ".")
	sidebarGit(t, repo, "commit", "-qm", "initial")
	sidebarGit(t, repo, "branch", "next")
	a.st.Workspaces[0].Cwd = repo
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Branch == "main" })
	if err := os.WriteFile(file, []byte("base\nnew\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 1 })
	sidebarGit(t, repo, "add", ".")
	sidebarGit(t, repo, "commit", "-qm", "next")
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 0 })
	sidebarGit(t, repo, "checkout", "-q", "next")
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Branch == "next" })
}

func TestSlowGitResultPreservesMovedWorkspaceStatus(t *testing.T) {
	teststate.Lock(t)
	root := t.TempDir()
	dirs := []string{filepath.Join(root, "slow"), filepath.Join(root, "old"), filepath.Join(root, "new")}
	for i, dir := range dirs {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		sidebarGit(t, dir, "init", "-qb", []string{"slowbranch", "oldbranch", "newbranch"}[i])
		if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte("one\n"), 0600); err != nil {
			t.Fatal(err)
		}
		sidebarGit(t, dir, "add", "tracked")
		sidebarGit(t, dir, "-c", "user.name=Test", "-c", "user.email=t@t", "commit", "-qm", "initial")
	}
	gatePath, readyPath := filepath.Join(root, "gate"), filepath.Join(root, "ready")
	if err := syscall.Mkfifo(gatePath, 0600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(readyPath, 0600); err != nil {
		t.Fatal(err)
	}
	gate, err := os.OpenFile(gatePath, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	defer func() { _, _ = gate.WriteString("go\n") }()
	hook, released := filepath.Join(root, "hook"), filepath.Join(root, "released")
	script := "#!/bin/sh\nif [ ! -f '" + released + "' ]; then\nprintf x > '" + readyPath + "'\nread x < '" + gatePath + "'\n: > '" + released + "'\nfi\nprintf '\\0'\n"
	if err := os.WriteFile(hook, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, dirs[0], "config", "core.fsmonitor", hook)
	ready := make(chan error, 1)
	go func() {
		f, err := os.Open(readyPath)
		if err == nil {
			var b [1]byte
			_, err = f.Read(b[:])
			f.Close()
		}
		ready <- err
	}()
	a := specRuntimeApp(t)
	defer a.closeGitActivity()
	a.st.Workspaces[0].Cwd = dirs[0]
	a.st.Apply(model.CmdNewWorkspace{Cwd: dirs[1], Name: "moving", NoFocus: true})
	a.refreshWorkspaceGit(time.Now())
	signaled := false
	for !signaled || a.workspaceGit[1].Branch != "oldbranch" {
		select {
		case err := <-ready:
			if err != nil {
				t.Fatal(err)
			}
			signaled = true
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		}
	}
	a.st.Workspaces[1].Cwd = dirs[2]
	a.refreshWorkspaceGit(a.gitAt.Add(1500 * time.Millisecond))
	for a.workspaceGit[1].Branch != "newbranch" {
		job := <-a.jobs
		job.fn()
		close(job.done)
	}
	_, _ = gate.WriteString("go\n")
	for a.gitActivity.running[dirs[0]] != nil {
		job := <-a.jobs
		job.fn()
		close(job.done)
	}
	if got := a.workspaceGit[1].Branch; got != "newbranch" {
		t.Fatalf("slow result erased moved workspace status: %q", got)
	}
}

func TestWorkspaceGitApplyWorkPerRepository(t *testing.T) {
	teststate.Lock(t)
	for _, count := range []int{4, 8} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			root := t.TempDir()
			a := specRuntimeApp(t)
			defer a.closeGitActivity()
			for i := 0; i < count; i++ {
				dir := filepath.Join(root, strconv.Itoa(i))
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				sidebarGit(t, dir, "init", "-qb", "measured")
				if i == 0 {
					a.st.Workspaces[0].Cwd = dir
				} else {
					a.st.Apply(model.CmdNewWorkspace{Cwd: dir, NoFocus: true})
				}
			}
			started := time.Now()
			a.refreshWorkspaceGit(started)
			for len(a.gitActivity.running) != 0 {
				job := <-a.jobs
				job.fn()
				close(job.done)
			}
			for i := 0; i < count; i++ {
				if a.workspaceGit[i].Branch != "measured" {
					t.Fatalf("workspace %d branch = %q", i, a.workspaceGit[i].Branch)
				}
			}
			t.Logf("repositories=%d elapsed=%s", count, time.Since(started))
		})
	}
}

func TestWorkspaceGitStatusFollowsIdentityAfterClose(t *testing.T) {
	teststate.Lock(t)
	root := t.TempDir()
	dirs := []string{filepath.Join(root, "first"), filepath.Join(root, "second")}
	for i, dir := range dirs {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		sidebarGit(t, dir, "init", "-qb", []string{"firstbranch", "secondbranch"}[i])
	}
	a := specRuntimeApp(t)
	defer a.closeGitActivity()
	a.st.Workspaces[0].Cwd = dirs[0]
	a.st.Apply(model.CmdNewWorkspace{Cwd: dirs[1], NoFocus: true})
	a.refreshWorkspaceGit(time.Now())
	for len(a.gitActivity.running) != 0 {
		job := <-a.jobs
		job.fn()
		close(job.done)
	}
	if a.workspaceGit[0].Branch != "firstbranch" || a.workspaceGit[1].Branch != "secondbranch" {
		t.Fatalf("initial status: %+v", a.workspaceGit)
	}
	a.exec(model.CmdCloseWorkspace{Index: 0})
	if got := a.workspaceGit[0].Branch; got != "secondbranch" {
		t.Fatalf("remaining workspace inherited removed workspace status: %q", got)
	}
}

func TestViewerWorkspaceGitStatusFollowsIdentityAfterClose(t *testing.T) {
	teststate.Lock(t)
	root := t.TempDir()
	dirs := []string{filepath.Join(root, "first"), filepath.Join(root, "second")}
	for i, dir := range dirs {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		sidebarGit(t, dir, "init", "-qb", []string{"firstbranch", "secondbranch"}[i])
	}
	a := specRuntimeApp(t)
	defer a.closeGitActivity()
	a.st.Workspaces[0].Cwd = dirs[0]
	a.st.Apply(model.CmdNewWorkspace{Cwd: dirs[1], NoFocus: true})
	a.refreshWorkspaceGit(time.Now())
	for len(a.gitActivity.running) != 0 {
		job := <-a.jobs
		job.fn()
		close(job.done)
	}
	if a.workspaceGit[0].Branch != "firstbranch" || a.workspaceGit[1].Branch != "secondbranch" {
		t.Fatalf("initial status: %+v", a.workspaceGit)
	}
	viewer := a.NewViewer(io.Discard, a.st.Cols, a.st.Rows, nil)
	viewer.with(a, func(ui *App) { ui.exec(model.CmdCloseWorkspace{Index: 0}) })
	if got := a.workspaceGit[0].Branch; got != "secondbranch" {
		t.Fatalf("remaining workspace inherited removed workspace status: %q", got)
	}
}
