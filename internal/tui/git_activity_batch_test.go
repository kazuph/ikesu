package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
