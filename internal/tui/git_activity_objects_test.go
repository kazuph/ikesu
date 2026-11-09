package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/filewatch"
)

// Loose objects, reflogs and hooks grow with every commit but never change
// the status shown for a cwd on their own. A repository with many commits
// must not put each object file into the per-pass stat list, and a new commit
// must still update the display through HEAD, refs and the index.
func TestGitActivitySkipsObjectStoreButSeesCommits(t *testing.T) {
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	for i := 0; i < 40; i++ {
		p := filepath.Join(repo, fmt.Sprintf("file-%03d", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("v%d\n", i)), 0600); err != nil {
			t.Fatal(err)
		}
		sidebarGit(t, repo, "add", ".")
		sidebarGit(t, repo, "commit", "-qm", fmt.Sprintf("c%d", i))
	}
	e := readGitActivity(context.Background(), repo, gitCwdActivity{watch: filewatch.New(nil)})
	defer e.watch.Close()
	if !e.ready {
		t.Fatal("watch unavailable")
	}
	real, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	objects := filepath.Join(real, ".git", "objects") + string(filepath.Separator)
	inObjects := 0
	for path := range e.paths {
		if strings.HasPrefix(path, objects) {
			inObjects++
		}
	}
	if inObjects != 0 {
		t.Fatalf("%d object store entries are checked on every pass; want 0", inObjects)
	}
	if err := os.WriteFile(filepath.Join(repo, "file-000"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sidebarGit(t, repo, "commit", "-qam", "next")
	sidebarGit(t, repo, "checkout", "-qb", "topic")
	e2, ok := unchangedGitActivity(e)
	if ok {
		t.Fatal("commit and branch switch were not detected")
	}
	e2 = readGitActivity(context.Background(), repo, e2)
	if e2.git.Branch != "topic" {
		t.Fatalf("branch after checkout = %q, want topic", e2.git.Branch)
	}
}
