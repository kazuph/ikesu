package tui

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/filewatch"
	"os"
	"path/filepath"
	"testing"
)

func TestGitActivityBaselineMeasurement(t *testing.T) {
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	for i := 0; i < 800; i++ {
		p := filepath.Join(repo, fmt.Sprintf("dir-%02d/file-%04d", i/20, i))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("before\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sidebarGit(t, repo, "add", ".")
	e := readGitActivity(repo, gitCwdActivity{watch: filewatch.New(nil)})
	defer e.watch.Close()
	if !e.ready {
		t.Fatal("watch unavailable")
	}
	t.Logf("full read paths=%d tracked=%d other=%d", len(e.paths), len(e.tracked), len(e.paths)-len(e.tracked))
	for n := 0; n < 4; n++ {
		var ok bool
		e, ok = unchangedGitActivity(e)
		t.Logf("quiet pass=%d tracked_stats=%d other_stats=%d unchanged=%v", n, e.trackedChecks, e.statChecks-e.trackedChecks, ok)
		if !ok {
			t.Fatal("unexpected change")
		}
	}
	// Write a file in the next checked batch, like an active editor.
	phase := (e.trackedNext&1)<<2 | e.trackedNext&2 | (e.trackedNext&4)>>2
	path := e.tracked[phase]
	if err := os.WriteFile(path, []byte("changed content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	e, ok := unchangedGitActivity(e)
	t.Logf("write detected=%v tracked_stats=%d other_stats=%d", !ok, e.trackedChecks, e.statChecks-e.trackedChecks)
	if ok {
		t.Fatal("change was not detected")
	}
	e = readGitActivity(repo, e)
	t.Logf("after one change full read paths=%d tracked=%d other=%d", len(e.paths), len(e.tracked), len(e.paths)-len(e.tracked))
}

func TestGitActivityQuietPassAvoidsRepeatingAllOtherStats(t *testing.T) {
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	for i := 0; i < 80; i++ {
		p := filepath.Join(repo, fmt.Sprintf("dir-%02d/file-%03d", i/10, i))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("before\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sidebarGit(t, repo, "add", ".")
	e := readGitActivity(repo, gitCwdActivity{watch: filewatch.New(nil)})
	defer e.watch.Close()
	if !e.ready {
		t.Fatal("watch unavailable")
	}
	e, ok := unchangedGitActivity(e)
	if !ok {
		t.Fatal("quiet repository changed")
	}
	other := len(e.paths) - len(e.tracked)
	checked := e.statChecks - e.trackedChecks
	t.Logf("quiet: other path stats=%d of %d", checked, other)
	if checked >= other {
		t.Fatalf("repeated all %d other path stats", other)
	}
}

func TestGitActivityOneContentChangeAvoidsFullSnapshot(t *testing.T) {
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	for i := 0; i < 80; i++ {
		p := filepath.Join(repo, fmt.Sprintf("file-%03d", i))
		if err := os.WriteFile(p, []byte("before\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sidebarGit(t, repo, "add", ".")
	sidebarGit(t, repo, "commit", "-qm", "initial")
	e := readGitActivity(repo, gitCwdActivity{watch: filewatch.New(nil)})
	defer e.watch.Close()
	if !e.ready {
		t.Fatal("watch unavailable")
	}
	if err := os.WriteFile(e.tracked[0], []byte("after\nextra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	e, ok := unchangedGitActivity(e)
	if ok {
		t.Fatal("change was not detected")
	}
	e = readGitActivity(repo, e)
	t.Logf("one change: snapshot stat=%d of %d paths, git +%d -%d", e.readChecks, len(e.paths), e.git.Added, e.git.Deleted)
	if e.git.Added != 2 || e.git.Deleted != 1 {
		t.Fatalf("wrong Git display: %+v", e.git)
	}
	if e.readChecks >= len(e.paths) {
		t.Fatalf("one change caused full snapshot of %d paths", e.readChecks)
	}
}
