package tui

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/worktree"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestG2WorktreeChildCompactParentGitDetailsActualRepository(t *testing.T) {
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	path := filepath.Join(repo, "file")
	if err := os.WriteFile(path, []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "add", ".")
	sidebarGit(t, repo, "commit", "-qm", "base")
	child := filepath.Join(t.TempDir(), "child")
	sidebarGit(t, repo, "worktree", "add", "-qb", "child", child)
	if err := os.WriteFile(path, []byte("base\nparent1\nparent2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "file"), []byte("base\nchild1\nchild2\nchild3\nchild4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	parentGit, childGit := readWorkspaceGit(repo), readWorkspaceGit(child)
	if parentGit.Added != 2 || childGit.Added != 4 {
		t.Fatal(parentGit, childGit)
	}
	s := model.New(repo, 120, 50)
	s.Workspaces[0].Worktree = &api.WorkspaceWorktreeInfo{RepoKey: repo}
	s.Apply(model.CmdNewWorkspace{Cwd: child})
	s.Workspaces[1].Worktree = &api.WorkspaceWorktreeInfo{RepoKey: repo, IsLinkedWorktree: true}
	for _, density := range []string{"full", "slim"} {
		s.WorkspaceDensity = density
		rows := render.SidebarRows(s, nil)
		childCount, parentDetails := 0, 0
		for _, r := range rows {
			if r.Ws == 1 && (r.Kind == render.RowWorkspace || r.Kind == render.RowGit || r.Kind == render.RowCardBlank) {
				childCount++
			}
			if r.Ws == 0 && r.Kind == render.RowGit {
				parentDetails++
			}
		}
		if childCount != 1 || parentDetails != 1 {
			t.Fatal(density, childCount, parentDetails)
		}
		frame := render.Compose(render.Input{State: s, Git: map[int]render.GitStatus{0: parentGit, 1: childGit}}).Frame
		var text strings.Builder
		for y := 0; y < frame.H; y++ {
			for x := 0; x < s.SidebarArea().W; x++ {
				if c := frame.Cell(x, y); c != nil {
					text.WriteString(c.Content)
				}
			}
		}
		if !strings.Contains(text.String(), "+2") || strings.Contains(text.String(), "+4") || strings.Contains(text.String(), "+6") {
			t.Fatal(text.String())
		}
	}
}
