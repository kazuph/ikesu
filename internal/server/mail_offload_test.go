package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestMailQueryDoesNotBlockAppLoop(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, dispatch.DBPathEnvVar, filepath.Join(t.TempDir(), "mail.db"))
	s := newTestServer(t, "mail-offload")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.InsertCommand("lock", "lock", "true", t.TempDir(), "p1", "p1", "none", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCommandRunning("lock", int64(os.Getpid())); err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	locked, release, cleanupDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		cleanupDone <- store.CleanupJobs(time.Now(), logDir, func(pid int) bool {
			close(locked)
			<-release
			return processGroupAlive(pid)
		})
	}()
	<-locked
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	flushStarted, flushDone := make(chan struct{}), make(chan struct{})
	go func() {
		s.app.Do(func() { close(flushStarted); s.flushMailForPane(1) })
		close(flushDone)
	}()
	<-flushStarted
	responsive := make(chan struct{})
	go s.app.Do(func() { close(responsive) })
	select {
	case <-responsive:
	case <-time.After(time.Second):
		t.Error("app loop blocked while mail database was locked")
	}
	close(release)
	if err := <-cleanupDone; err != nil {
		t.Error(err)
	}
	<-flushDone
	<-responsive
	s.app.Stop()
	<-appDone
	s.sup.KillAll()
}
