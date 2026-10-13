package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	b, e := exec.Command("git", append([]string{"-C", cwd}, args...)...).CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
	return string(b)
}
func repository(t *testing.T) string {
	t.Helper()
	r := filepath.Join(t.TempDir(), "repo with spaces")
	if e := os.Mkdir(r, 0700); e != nil {
		t.Fatal(e)
	}
	runGit(t, r, "init", "-q")
	runGit(t, r, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "init")
	return r
}
func TestWorktreeCreateExistingAndSafeRemoval(t *testing.T) {
	r := repository(t)
	path := filepath.Join(t.TempDir(), "checkout\nwith whitespace")
	if e := Create(r, path, "feat/new", ""); e != nil {
		t.Fatal(e)
	}
	items, e := List(r)
	if e != nil || len(items) != 2 {
		t.Fatalf("%+v %v", items, e)
	}
	if !SamePath(items[1].Path, path) || items[1].Branch == nil || *items[1].Branch != "feat/new" || !items[1].IsLinkedWorktree {
		t.Fatalf("%+v", items[1])
	}
	src, e := Source(path)
	if e != nil || !SamePath(src.RepoRoot, r) {
		t.Fatalf("%+v %v", src, e)
	}
	if e := os.WriteFile(filepath.Join(path, "unsaved"), []byte("keep me"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := Remove(r, path, false); e == nil {
		t.Fatal("dirty removal succeeded")
	}
	if b, e := os.ReadFile(filepath.Join(path, "unsaved")); e != nil || string(b) != "keep me" {
		t.Fatal("dirty data lost")
	}
	if e := Remove(r, path, true); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("checkout survived")
	}
	if e := Create(r, path, "feat/new", ""); e != nil {
		t.Fatal("existing branch checkout:", e)
	}
	runGit(t, r, "worktree", "lock", path)
	if e := Remove(r, path, true); e == nil {
		t.Fatal("locked checkout removal succeeded")
	}
	runGit(t, r, "worktree", "unlock", path)
	if e := Remove(r, path, false); e != nil {
		t.Fatal(e)
	}
	if e := Remove(r, r, true); e == nil {
		t.Fatal("main checkout removal succeeded")
	}
}
func TestBareRepositoryWorktree(t *testing.T) {
	r := repository(t)
	bare := filepath.Join(t.TempDir(), "repo.git")
	runGit(t, r, "clone", "--bare", r, bare)
	path := filepath.Join(t.TempDir(), "linked")
	if e := Create(bare, path, "bare/new", ""); e != nil {
		t.Fatal(e)
	}
	src, e := Source(bare)
	if e != nil || !SamePath(src.RepoRoot, bare) || src.RepoName != "repo" {
		t.Fatalf("%+v %v", src, e)
	}
	items, e := List(bare)
	if e != nil || len(items) != 2 || !items[0].IsBare || !items[1].IsLinkedWorktree {
		t.Fatalf("%+v %v", items, e)
	}
	if e := Remove(bare, path, false); e != nil {
		t.Fatal(e)
	}
}
func TestWorktreeCreateFailurePreservesCheckout(t *testing.T) {
	r := repository(t)
	path := filepath.Join(t.TempDir(), "checkout")
	if e := Create(r, path, "feat/one", ""); e != nil {
		t.Fatal(e)
	}
	if e := Create(r, path, "feat/two", ""); e == nil {
		t.Fatal("overwrote existing directory")
	}
	if _, e := os.Stat(filepath.Join(path, ".git")); e != nil {
		t.Fatal(e)
	}
	if e := Create(r, filepath.Join(t.TempDir(), "bad"), "--bad", ""); e == nil {
		t.Fatal("invalid branch accepted")
	}
}

func TestWorktreePorcelainPreservesFieldLikePathLines(t *testing.T) {
	r := repository(t)
	path := filepath.Join(t.TempDir(), "quoted\"\\日本語\n\nworktree decoy\nbranch refs/heads/decoy\nHEAD deadbeef")
	if err := Create(r, path, "feat/path-data", ""); err != nil {
		t.Fatal(err)
	}
	items, err := List(r)
	if err != nil || len(items) != 2 || !SamePath(items[1].Path, path) || items[1].Branch == nil || *items[1].Branch != "feat/path-data" {
		t.Fatal(items, err)
	}
	if err := Remove(r, path, false); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeListUsesNULUnlessGitRejectsOption(t *testing.T) {
	r := repository(t)
	path := filepath.Join(t.TempDir(), "checkout\nwith whitespace")
	if err := Create(r, path, "feat/format", ""); err != nil {
		t.Fatal(err)
	}
	_, probeErr := git(r, "worktree", "list", "--porcelain", "-z")
	trace := filepath.Join(t.TempDir(), "git-trace")
	t.Setenv("GIT_TRACE", trace)
	items, err := List(r)
	if err != nil || len(items) != 2 || !SamePath(items[1].Path, path) {
		t.Fatal(items, err)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	if !strings.Contains(log, "worktree list --porcelain -z") {
		t.Fatal("NUL format not attempted", log)
	}
	if probeErr == nil {
		if strings.Contains(log, "--git-common-dir") {
			t.Fatal("supported Git used legacy registry parser", log)
		}
		t.Log("real Git accepted -z; List used only the original NUL path")
	} else {
		if !strings.Contains(log, "--git-common-dir") {
			t.Fatal("old Git did not use registry parser", log)
		}
		t.Log("real Git rejected -z; List used the legacy path after the usage error")
	}
	if _, err := List(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("invalid repository must remain an error")
	}
}
