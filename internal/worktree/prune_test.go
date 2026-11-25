package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPruneRequiresLinkedMergedCleanCheckout(t *testing.T) {
	ctx := context.Background()
	root := repository(t)
	runGit(t, root, "branch", "-M", "main")
	top := t.TempDir()
	path := filepath.Join(top, "merged")
	runGit(t, root, "worktree", "add", "-qb", "feature", path)
	runGit(t, path, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "feature")
	head := strings.TrimSpace(runGit(t, path, "rev-parse", "HEAD"))
	runGit(t, root, "update-ref", "refs/remotes/origin/main", "main")
	runGit(t, root, "merge", "--ff-only", "feature")
	check := func(checkout, want string) PruneRevision {
		t.Helper()
		repo, err := ReadPruneRepository(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		rev, reason, err := repo.Check(ctx, checkout)
		if err != nil || reason != want {
			t.Fatalf("check %q: %+v, %q, %v, expected %q", checkout, rev, reason, err, want)
		}
		return rev
	}
	check(root, "not a linked worktree")
	check(path, "unmerged") // local main has it; origin/main does not.
	runGit(t, root, "update-ref", "refs/remotes/origin/main", "main")
	if got := check(path, ""); got != (PruneRevision{"feature", head, "origin/main"}) {
		t.Fatalf("revision = %+v", got)
	}

	// origin/HEAD wins over main, with a slash-bearing default branch name.
	runGit(t, root, "update-ref", "refs/remotes/origin/release/stable", "main")
	runGit(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/release/stable")
	if got := check(path, ""); got.Base != "origin/release/stable" {
		t.Fatalf("default = %s", got.Base)
	}
	basePath := filepath.Join(top, "base")
	runGit(t, root, "worktree", "add", "-qb", "release/stable", basePath)
	check(basePath, "default branch")
	runGit(t, root, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
	runGit(t, root, "update-ref", "-d", "refs/remotes/origin/main")
	runGit(t, root, "branch", "-M", "master")
	if got := check(path, ""); got.Base != "master" {
		t.Fatalf("fallback = %s", got.Base)
	}
	runGit(t, root, "worktree", "lock", path)
	check(path, "locked worktree")
	runGit(t, root, "worktree", "unlock", path)

	draft := filepath.Join(path, "untracked")
	if err := os.WriteFile(draft, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	check(path, "dirty")
	if err := os.Remove(draft); err != nil {
		t.Fatal(err)
	}
	runGit(t, path, "checkout", "--detach", "HEAD")
	check(path, "detached HEAD")
}

func TestPruneDiskUsageCountsFilesWithoutFollowingSymlinks(t *testing.T) {
	ctx := context.Background()
	path, outside := t.TempDir(), t.TempDir()
	write := func(dir, name string, n int) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("x", n)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	usage := func(p string) uint64 {
		t.Helper()
		n, err := DiskUsage(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	empty := usage(path)
	write(path, "one", 128*1024)
	write(path, "two", 256*1024)
	local := usage(path)
	if local < empty+384*1024 {
		t.Fatalf("allocation not counted: before=%d after=%d", empty, local)
	}
	write(outside, "large", 4*1024*1024)
	if err := os.Symlink(outside, filepath.Join(path, "external")); err != nil {
		t.Fatal(err)
	}
	if withLink := usage(path); withLink < local || withLink >= local+usage(outside) {
		t.Fatalf("symlink target counted: before=%d after=%d", local, withLink)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := DiskUsage(canceled, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
