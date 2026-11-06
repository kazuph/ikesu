package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/teststate"
)

func TestWorkspaceGitLargeRepoReflectsChanges(t *testing.T) {
	teststate.Lock(t)
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	for i := 0; i < 1000; i++ {
		if err := os.WriteFile(filepath.Join(repo, fmt.Sprintf("%04d.txt", i)), []byte("base\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sidebarGit(t, repo, "add", ".")
	sidebarGit(t, repo, "commit", "-qm", "base")
	a := specRuntimeApp(t)
	defer a.closeGitActivity()
	a.st.Workspaces[0].Cwd = repo
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Branch == "main" })
	if err := os.WriteFile(filepath.Join(repo, "0500.txt"), []byte("changed\nextra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Added == 2 && a.workspaceGit[0].Deleted == 1 })
}
