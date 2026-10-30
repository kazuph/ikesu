package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBranchCacheExternalGitDirAncestorRecovery(t *testing.T) {
	for _, initiallyMissing := range []bool{false, true} {
		t.Run(map[bool]string{false: "removed", true: "initially missing"}[initiallyMissing], func(t *testing.T) {
			root := t.TempDir()
			cwd := filepath.Join(root, "checkout")
			parent := filepath.Join(root, "storage", "repositories", "main")
			gitdir := filepath.Join(parent, "git", "worktrees", "topic")
			for _, dir := range []string{cwd, filepath.Dir(parent)} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(cwd, ".git"), []byte("gitdir: "+gitdir+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			create := func() {
				if err := os.MkdirAll(gitdir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(gitdir, "HEAD"), []byte("ref: refs/heads/topic\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if !initiallyMissing {
				create()
			}
			a := &App{}
			t.Cleanup(func() { a.pruneBranchCache(nil) })
			want := "topic"
			if initiallyMissing {
				want = ""
			}
			if got := a.cachedGitBranch(cwd); got != want {
				t.Fatalf("initial=%q want=%q", got, want)
			}
			wait := func(want string) {
				t.Helper()
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					if a.cachedGitBranch(cwd) == want {
						return
					}
					time.Sleep(time.Millisecond)
				}
				t.Fatalf("branch did not become %q", want)
			}
			if !initiallyMissing {
				if err := os.Rename(parent, parent+".saved"); err != nil {
					t.Fatal(err)
				}
				// Force the intermediate empty cache even on the old implementation.
				gitfile := filepath.Join(cwd, ".git")
				if err := os.Chtimes(gitfile, time.Now(), time.Now()); err != nil {
					t.Fatal(err)
				}
				wait("")
				// Drain the removal notifications before testing restoration.
				entry := a.branchCache[filepath.Clean(cwd)]
				last := entry.watch.Revision()
				stableSince := time.Now()
				for time.Since(stableSince) < 50*time.Millisecond {
					time.Sleep(time.Millisecond)
					if revision := entry.watch.Revision(); revision != last {
						last, stableSince = revision, time.Now()
						_ = a.cachedGitBranch(cwd)
					}
				}
				if err := os.Rename(parent+".saved", parent); err != nil {
					t.Fatal(err)
				}
			} else {
				create()
			}
			wait("topic")
		})
	}
}

func TestBranchCacheExternalSymlinkTargetRecovery(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "checkout")
	parent := filepath.Join(root, "storage", "main")
	link := filepath.Join(root, "link")
	gitdir := filepath.Join(parent, "git", "topic")
	for _, dir := range []string{cwd, gitdir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(parent, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".git"), []byte("gitdir: "+filepath.Join(link, "git", "topic")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitdir, "HEAD"), []byte("ref: refs/heads/topic\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	t.Cleanup(func() { a.pruneBranchCache(nil) })
	if a.cachedGitBranch(cwd) != "topic" {
		t.Fatal("initial branch missing")
	}
	wait := func(want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if a.cachedGitBranch(cwd) == want {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("branch did not become %q", want)
	}
	if err := os.Rename(parent, parent+".saved"); err != nil {
		t.Fatal(err)
	}
	wait("")
	if err := os.Rename(parent+".saved", parent); err != nil {
		t.Fatal(err)
	}
	wait("topic")
}
