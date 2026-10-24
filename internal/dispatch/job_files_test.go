package dispatch

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestJobDiagnosticsShareRetentionAndCannotResurrect(t *testing.T) {
	s := testStore(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "owned.log")
	now := time.Now().UTC()
	for _, suffix := range jobFileSuffixes {
		if err := os.WriteFile(log+suffix, []byte("owned"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(log+suffix, now.Add(-2*JobRetention), now.Add(-2*JobRetention)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.InsertCommand("owned", "owned", "true", dir, "p1", "owner", "none", log); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCommandRunning("owned", int64(os.Getpid())); err != nil {
		t.Fatal(err)
	}
	alive := func(pid int) bool { return syscall.Kill(pid, 0) == nil }
	if err := s.CleanupJobs(now.Add(JobRetention), dir, alive); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range jobFileSuffixes {
		if _, err := os.Stat(log + suffix); err != nil {
			t.Fatal("active diagnostic removed", suffix, err)
		}
	}
	if _, err := s.MarkCommandFinished("owned", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanupJobs(now.Add(2*JobRetention), dir, alive); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range jobFileSuffixes {
		if _, err := os.Stat(log + suffix); !os.IsNotExist(err) {
			t.Fatal("expired diagnostic retained", suffix, err)
		}
	}
	called := false
	err := WithJobLogLock(log, func() error { called = true; return os.WriteFile(log+".monitor-exit.json", []byte("late"), 0600) })
	if !os.IsNotExist(err) || called {
		t.Fatal("late writer recreated retired job", err, called)
	}
}

func TestDiagnosticPublicationSerializesWithCleanup(t *testing.T) {
	log := filepath.Join(t.TempDir(), "job.log")
	if err := os.WriteFile(log, nil, 0600); err != nil {
		t.Fatal(err)
	}
	locked, release := make(chan struct{}), make(chan struct{})
	published := make(chan error, 1)
	go func() {
		published <- WithJobLogLock(log, func() error {
			close(locked)
			<-release
			return os.WriteFile(log+".monitor-exit.json", []byte("diagnosis"), 0600)
		})
	}()
	<-locked
	removed := make(chan error, 1)
	go func() { removed <- removeJobFiles(log) }()
	close(release)
	if err := <-published; err != nil {
		t.Fatal(err)
	}
	if err := <-removed; err != nil {
		t.Fatal(err)
	}
	for _, suffix := range jobFileSuffixes {
		if _, err := os.Stat(log + suffix); !os.IsNotExist(err) {
			t.Fatal("publication resurrected cleanup", suffix, err)
		}
	}
}

func TestOrphanCleanupRemovesRelatedLogsWithoutFalseError(t *testing.T) {
	s := testStore(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "orphan.log")
	now := time.Now().UTC()
	for _, suffix := range jobFileSuffixes {
		path := log + suffix
		if err := os.WriteFile(path, []byte("orphan"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-2*JobRetention), now.Add(-2*JobRetention)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CleanupJobs(now, dir, func(pid int) bool { return syscall.Kill(pid, 0) == nil }); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("orphan diagnostics retained", entries, err)
	}
}
