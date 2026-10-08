package tui

import (
	"github.com/kazuph/goterrace/internal/worktree"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func sidebarGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	b, e := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
}
func TestG2GitNumstatBinaryAndLinkedName(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "main-repository")
	if e := os.Mkdir(repo, 0700); e != nil {
		t.Fatal(e)
	}
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	p := filepath.Join(repo, "text")
	if e := os.WriteFile(p, []byte("a\nb\nc\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(repo, "binary"), []byte{0, 1, 2}, 0600); e != nil {
		t.Fatal(e)
	}
	sidebarGit(t, repo, "add", ".")
	sidebarGit(t, repo, "commit", "-qm", "base")
	if e := os.WriteFile(p, []byte("a\nz\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(repo, "new"), []byte("x\ny\n"), 0600); e != nil {
		t.Fatal(e)
	}
	sidebarGit(t, repo, "add", "new")
	if e := os.WriteFile(filepath.Join(repo, "binary"), []byte{0, 2, 3}, 0600); e != nil {
		t.Fatal(e)
	}
	got := readWorkspaceGit(repo)
	if got.Branch != "main" || got.Added != 3 || got.Deleted != 2 {
		t.Fatal(got)
	}
	linked := filepath.Join(t.TempDir(), "checkout")
	sidebarGit(t, repo, "worktree", "add", "-qb", "linked", linked)
	src, e := worktree.Source(linked)
	if e != nil || src.RepoName != "main-repository" {
		t.Fatal(src, e)
	}
}
func TestG2GitAheadBehindActualRepository(t *testing.T) {
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	sidebarGit(t, repo, "commit", "--allow-empty", "-qm", "base")
	sidebarGit(t, repo, "branch", "upstream")
	sidebarGit(t, repo, "checkout", "-q", "upstream")
	if e := os.WriteFile(filepath.Join(repo, "upstream-file"), []byte("upstream\n"), 0600); e != nil {
		t.Fatal(e)
	}
	sidebarGit(t, repo, "add", ".")
	sidebarGit(t, repo, "commit", "-qm", "upstream")
	sidebarGit(t, repo, "checkout", "-q", "main")
	for _, msg := range []string{"one", "two"} {
		sidebarGit(t, repo, "commit", "--allow-empty", "-qm", msg)
	}
	sidebarGit(t, repo, "branch", "--set-upstream-to=upstream", "main")
	g := readWorkspaceGit(repo)
	if g.Ahead != 2 || g.Behind != 1 {
		t.Fatal(g)
	}
}
