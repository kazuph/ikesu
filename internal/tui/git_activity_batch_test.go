package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/teststate"

	"github.com/kazuph/ikesu/internal/filewatch"
)

func TestGitActivityDoesNotProbeEveryTrackedFileEachRefresh(t *testing.T) {
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	for i := 0; i < 80; i++ {
		path := filepath.Join(repo, fmt.Sprintf("file-%03d", i))
		if err := os.WriteFile(path, []byte("before\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sidebarGit(t, repo, "add", ".")
	entry := readGitActivity(repo, gitCwdActivity{watch: filewatch.New(nil)})
	defer entry.watch.Close()
	if !entry.ready {
		t.Fatal("watch unavailable")
	}
	entry, unchanged := unchangedGitActivity(entry)
	t.Logf("unchanged refresh: %d tracked file stats of 80; %d total stats", entry.trackedChecks, entry.statChecks)
	if entry.trackedChecks > 27 {
		t.Fatalf("probed %d of 80 tracked files", entry.trackedChecks)
	}
	if !unchanged {
		t.Fatal("quiet repository changed")
	}
	if err := os.WriteFile(filepath.Join(repo, "file-079"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, unchanged = unchangedGitActivity(entry)
	if !unchanged {
		t.Fatal("first refresh probed the last of 80 tracked files")
	}
	found := false
	for i := 0; i < 9; i++ {
		var ok bool
		entry, ok = unchangedGitActivity(entry)
		if !ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("tracked write was not detected within 10 refreshes")
	}
}

// The last batch exercises the real 1.5-second refresh cadence for up to 12 seconds.
func TestWorkspaceGitContentOnlyRefreshWithinFifteenSeconds(t *testing.T) {
	teststate.Lock(t)
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	for i := 0; i < 80; i++ {
		if err := os.WriteFile(filepath.Join(repo, fmt.Sprintf("file-%03d", i)), []byte("before\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sidebarGit(t, repo, "add", ".")
	sidebarGit(t, repo, "commit", "-qm", "initial")
	a := specRuntimeApp(t)
	defer a.closeGitActivity()
	a.st.Workspaces[0].Cwd = repo
	waitWorkspaceGitActivity(t, a, func() bool { return a.workspaceGit[0].Branch == "main" })
	if err := os.WriteFile(filepath.Join(repo, "file-007"), []byte("after\nextra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		a.refreshWorkspaceGit(time.Now())
		finishGitRefresh(t, a)
		if a.workspaceGit[0].Added == 2 && a.workspaceGit[0].Deleted == 1 {
			t.Logf("content-only write visible after %s", time.Since(started))
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("content-only write not visible within 15 seconds: %+v", a.workspaceGit[0])
		case <-tick.C:
		}
	}
}
