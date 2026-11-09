package tui

import (
	"context"
	"fmt"
	"github.com/kazuph/ikesu/internal/filewatch"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	e := readGitActivity(context.Background(), repo, gitCwdActivity{watch: filewatch.New(nil)})
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
	e = readGitActivity(context.Background(), repo, e)
	t.Logf("after one change cached paths=%d tracked=%d other=%d", len(e.paths), len(e.tracked), len(e.paths)-len(e.tracked))
}

func TestGitActivityQuietPassAvoidsRepeatingAllOtherStats(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
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
	e := readGitActivity(context.Background(), repo, gitCwdActivity{watch: filewatch.New(nil)})
	defer e.watch.Close()
	if !e.ready {
		t.Fatal("watch unavailable")
	}
	other := len(e.paths) - len(e.tracked)
	// A sibling write wakes an ancestor watch but does not change the repo.
	// It makes the first pass legitimately check every directory.
	revision := e.watch.Revision()
	if err := os.Mkdir(filepath.Join(parent, "sibling"), 0700); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for e.watch.Revision() == revision {
		if time.Now().After(deadline) {
			t.Fatal("sibling change did not reach the native watcher")
		}
		time.Sleep(time.Millisecond)
	}
	var ok bool
	e, ok = unchangedGitActivity(e)
	if !ok {
		t.Fatal("unrelated sibling changed the repository")
	}
	if checked := e.statChecks - e.trackedChecks; checked != other {
		t.Fatalf("notification pass checked %d of %d other paths", checked, other)
	} else {
		t.Logf("notification: other path stats=%d of %d", checked, other)
	}
	// Other tests can write to shared ancestors. Assert the original strict
	// reduction on a pass that actually saw no new notification and is not
	// the mandatory eighth directory check.
	for time.Now().Before(deadline) {
		previous, phase := e.revision, e.trackedNext
		e, ok = unchangedGitActivity(e)
		if !ok {
			t.Fatal("quiet repository changed")
		}
		if e.revision != previous || phase == 7 {
			continue
		}
		checked := e.statChecks - e.trackedChecks
		t.Logf("quiet: other path stats=%d of %d", checked, other)
		if checked >= other {
			t.Fatalf("repeated all %d other path stats", other)
		}
		return
	}
	t.Fatal("no notification-free pass before deadline")
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
	e := readGitActivity(context.Background(), repo, gitCwdActivity{watch: filewatch.New(nil)})
	defer e.watch.Close()
	if !e.ready {
		t.Fatal("watch unavailable")
	}
	name := e.name
	if err := os.WriteFile(e.tracked[0], []byte("after\nextra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	e, ok := unchangedGitActivity(e)
	if ok {
		t.Fatal("change was not detected")
	}
	e = readGitActivity(context.Background(), repo, e)
	t.Logf("one change: snapshot stat=%d of %d paths, git +%d -%d", e.readChecks, len(e.paths), e.git.Added, e.git.Deleted)
	if e.git.Added != 2 || e.git.Deleted != 1 {
		t.Fatalf("wrong Git display: %+v", e.git)
	}
	if e.git.Branch != "main" || e.name != name {
		t.Fatalf("content edit changed branch or workspace name: %+v, %q", e.git, e.name)
	}
	if e.readChecks >= len(e.paths) {
		t.Fatalf("one change caused full snapshot of %d paths", e.readChecks)
	}
}

// A 1.5-second cadence with an edit every two passes represents 60 seconds.
func TestGitActivityBaselineReloadFrequency(t *testing.T) {
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	for i := 0; i < 80; i++ {
		p := filepath.Join(repo, fmt.Sprintf("file-%03d", i))
		if err := os.WriteFile(p, []byte("before\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sidebarGit(t, repo, "add", ".")
	e := readGitActivity(context.Background(), repo, gitCwdActivity{watch: filewatch.New(nil)})
	defer e.watch.Close()
	if !e.ready {
		t.Fatal("watch unavailable")
	}
	full, writes, stats, snapshotStats := 0, 0, 0, 0
	for pass := 0; pass < 40; pass++ {
		if pass%2 == 0 {
			p := e.tracked[(pass*17/2)%len(e.tracked)]
			if err := os.WriteFile(p, []byte(fmt.Sprintf("change-%d\n", pass)), 0600); err != nil {
				t.Fatal(err)
			}
			writes++
		}
		var ok bool
		e, ok = unchangedGitActivity(e)
		stats += e.statChecks
		if !ok {
			e = readGitActivity(context.Background(), repo, e)
			snapshotStats += e.readChecks
			if e.readChecks >= len(e.paths) {
				full++
			}
		}
	}
	t.Logf("40 simulated 1.5s passes, writes=%d, full_reads=%d, probe_stats=%d, snapshot_stats=%d", writes, full, stats, snapshotStats)
}

func TestGitActivityContinuousWritesDoNotStarveLaterBatches(t *testing.T) {
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
	e := readGitActivity(context.Background(), repo, gitCwdActivity{watch: filewatch.New(nil)})
	defer e.watch.Close()
	if !e.ready {
		t.Fatal("watch unavailable")
	}
	// Write the last batch before a stream of edits in the current batch.
	if err := os.WriteFile(e.tracked[7], []byte("after\nextra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 8; pass++ {
		phase := (e.trackedNext&1)<<2 | e.trackedNext&2 | (e.trackedNext&4)>>2
		if phase != 7 {
			if err := os.WriteFile(e.tracked[phase], []byte("after\nextra\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		var ok bool
		e, ok = unchangedGitActivity(e)
		if ok {
			t.Fatalf("pass %d missed its changed batch", pass)
		}
		e = readGitActivity(context.Background(), repo, e)
	}
	if e.git.Added != 16 || e.git.Deleted != 8 {
		t.Fatalf("continuous edits missed last batch: %+v", e.git)
	}
}

// The real 1.5-second cadence is needed to verify the 15-second deadline.
func TestGitActivityDirectoryFallbackWithinFifteenSeconds(t *testing.T) {
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	sidebarGit(t, repo, "config", "user.name", "Test")
	sidebarGit(t, repo, "config", "user.email", "test@example.invalid")
	dir := filepath.Join(repo, "nested")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file-%02d", i)), []byte("before\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sidebarGit(t, repo, "add", ".")
	sidebarGit(t, repo, "commit", "-qm", "initial")
	e := readGitActivity(context.Background(), repo, gitCwdActivity{watch: filewatch.New(nil)})
	if !e.ready {
		e.watch.Close()
		t.Fatal("watch unavailable")
	}
	e.watch.Close()
	e.revision = e.watch.Revision()
	var ok bool
	e, ok = unchangedGitActivity(e)
	if !ok {
		t.Fatal("initial repository was not quiet")
	}
	if err := os.WriteFile(filepath.Join(dir, "new-untracked"), []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.tracked[0], []byte("after\nextra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	revision := e.watch.Revision()
	started := time.Now()
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for pass := 1; pass <= 7; pass++ {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("directory fallback exceeded 15 seconds")
		}
		if e.watch.Revision() != revision {
			t.Fatal("closed watcher delivered a notification")
		}
		e, ok = unchangedGitActivity(e)
		if pass < 7 && !ok {
			t.Fatalf("directory change detected before fallback at pass %d", pass)
		}
		if pass == 7 {
			if ok {
				t.Fatal("eighth directory check missed change")
			}
			e = readGitActivity(context.Background(), repo, e)
			if e.git.Added != 2 || e.git.Deleted != 1 {
				t.Fatalf("directory fallback did not update Git display: %+v", e.git)
			}
			if elapsed := time.Since(started); elapsed > 15*time.Second {
				t.Fatalf("directory fallback took %s", elapsed)
			} else {
				t.Logf("eighth check updated Git display after %s", elapsed)
			}
		}
	}
}
