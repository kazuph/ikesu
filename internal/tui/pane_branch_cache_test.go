package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/filewatch"
)

func TestBranchCacheRealGitChanges(t *testing.T) {
	a := &App{}
	t.Cleanup(func() { a.pruneBranchCache(nil) })
	repo := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git(repo, "init", "-q")
	git(repo, "symbolic-ref", "HEAD", "refs/heads/first")
	git(repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "initial")
	check := func(dir, want string) {
		t.Helper()
		if got := a.cachedGitBranch(dir); got != want {
			t.Fatalf("cwd %s: got %q, want %q", dir, got, want)
		}
	}
	waitChange := func(dir string, action func()) {
		t.Helper()
		entry := a.branchCache[filepath.Clean(dir)]
		revision := entry.watch.Revision()
		action()
		deadline := time.Now().Add(3 * time.Second)
		for entry.watch.Revision() == revision && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if entry.watch.Revision() == revision {
			t.Fatal("file change notification did not arrive")
		}
	}
	check(repo, "first")
	waitChange(repo, func() { git(repo, "switch", "-qc", "second") })
	check(repo, "second")
	waitChange(repo, func() { git(repo, "switch", "--detach", "-q") })
	check(repo, "")
	waitChange(repo, func() { git(repo, "switch", "-q", "first") })
	check(repo, "first")
	head := filepath.Join(repo, ".git", "HEAD")
	waitChange(repo, func() {
		if err := os.Rename(head, head+".saved"); err != nil {
			t.Fatal(err)
		}
	})
	check(repo, "")
	waitChange(repo, func() {
		if err := os.Rename(head+".saved", head); err != nil {
			t.Fatal(err)
		}
	})
	check(repo, "first")
	linked := filepath.Join(t.TempDir(), "linked")
	check(linked, "")
	waitChange(linked, func() { git(repo, "worktree", "add", "-qb", "linked", linked) })
	check(linked, "linked")
	waitChange(linked, func() { git(linked, "switch", "-qc", "linked-next") })
	check(linked, "linked-next")
	waitChange(linked, func() { git(repo, "worktree", "remove", "--force", linked) })
	check(linked, "")
	nongit := t.TempDir()
	check(nongit, "")
	waitChange(nongit, func() { git(nongit, "init", "-q", "-b", "new") })
	check(nongit, "new")
	waitChange(nongit, func() {
		if err := os.Rename(filepath.Join(nongit, ".git"), filepath.Join(nongit, "saved-git")); err != nil {
			t.Fatal(err)
		}
	})
	check(nongit, "")
	a.pruneBranchCache(map[string]bool{repo: true})
	if len(a.branchCache) != 1 {
		t.Fatalf("unused entries retained: %d", len(a.branchCache))
	}
}

func TestBranchCacheClosedWatcherReadsEachTime(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	head := filepath.Join(repo, ".git", "HEAD")
	if err := os.WriteFile(head, []byte("ref: refs/heads/one\n"), 0600); err != nil {
		t.Fatal(err)
	}
	watch := filewatch.New(nil)
	watch.Close()
	a := &App{branchCache: map[string]*branchCacheEntry{repo: {watch: watch}}}
	if got := a.cachedGitBranch(repo); got != "one" {
		t.Fatal(got)
	}
	if err := os.WriteFile(head, []byte("ref: refs/heads/two\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := a.cachedGitBranch(repo); got != "two" {
		t.Fatal(got)
	}
}
