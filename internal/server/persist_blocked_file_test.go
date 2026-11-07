package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/kazuph/ikesu/internal/teststate"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestScheduledSessionBlockedFileDoesNotStopAppLoop(t *testing.T) {
	teststate.Lock(t)
	root := t.TempDir()
	teststate.Setenv(t, "CODEX_HOME", root)
	s := newTestServer(t, "blocked-observation")
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	p := s.sup.Get(s.app.State().Workspaces[0].Tabs[0].Layout.Focused())
	p.SetAgent("codex")
	path := filepath.Join(root, "sessions", "rollout-blocked.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	writer := -1
	defer func() {
		if writer >= 0 {
			_ = unix.Close(writer)
		}
		s.app.Stop()
		<-appDone
		s.sup.KillAll()
	}()
	saved := make(chan struct{})
	go func() { s.saveScheduledSession(); close(saved) }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for writer < 0 {
		fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0)
		if err == nil {
			writer = fd
			break
		}
		if !errors.Is(err, unix.ENXIO) {
			t.Fatal(err)
		}
		select {
		case <-saved:
			t.Fatal("session observation ended before opening the file")
		case <-deadline.C:
			t.Fatal("session observation did not open the file")
		case <-tick.C:
		}
	}
	responsive := make(chan struct{})
	go s.app.Do(func() { close(responsive) })
	select {
	case <-responsive:
	case <-time.After(tui.LoopStallThreshold):
		t.Error("session file read stopped the app loop for the stall threshold")
	}
	_ = unix.Close(writer)
	writer = -1
	select {
	case <-saved:
	case <-deadline.C:
		t.Fatal("session save did not finish after the file was released")
	}
}
