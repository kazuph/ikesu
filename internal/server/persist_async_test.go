package server

import (
	"os"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestScheduledSessionWriteDoesNotHoldAppLoop(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "async-save")
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		t.Fatal(err)
	}
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	defer func() { s.app.Stop(); <-appDone }()
	s.saveWriteMu.Lock()
	locked := true
	defer func() {
		if locked {
			s.saveWriteMu.Unlock()
		}
	}()
	written := make(chan struct{})
	go func() { s.saveScheduledSession(); close(written) }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		captured := make(chan uint64, 1)
		go s.app.Do(func() { captured <- s.saveSequence })
		select {
		case seq := <-captured:
			if seq == 0 {
				continue
			}
		case <-deadline.C:
			t.Fatal("disk writer held the model loop")
		}
		break
	}
	select {
	case <-written:
		t.Fatal("writer bypassed persistence serialization")
	default:
	}
	s.saveWriteMu.Unlock()
	locked = false
	select {
	case <-written:
	case <-deadline.C:
		t.Fatal("scheduled write did not complete after serialization released")
	}
	if f, err := session.Load(session.Path(s.dir)); err != nil || f == nil {
		t.Fatal("scheduled write was lost", f, err)
	}
}

func TestSessionWriteOrderFinalSaveAndFailurePreserveNewestSnapshot(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "ordered-save")
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		t.Fatal(err)
	}
	s.app.State().Workspaces[0].Name = "OLDER"
	older := s.captureSession()
	s.app.State().Workspaces[0].Name = "FINAL"
	s.saveSession()
	s.writeSession(older)
	check := func(want string) {
		t.Helper()
		f, err := session.Load(session.Path(s.dir))
		if err != nil || f == nil || f.Workspaces[0].Name != want {
			t.Fatal("delayed write replaced the final state", f, err)
		}
	}
	check("FINAL")
	s.app.State().Workspaces[0].Name = "RETRY"
	retry := s.captureSession()
	previous := s.savedSequence
	if err := os.Rename(s.dir, s.dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.dir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.writeSession(retry)
	if s.savedSequence != previous {
		t.Fatal("failed filesystem write advanced the committed generation")
	}
	if err := os.Remove(s.dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(s.dir+"-saved", s.dir); err != nil {
		t.Fatal(err)
	}
	check("FINAL")
	s.writeSession(retry)
	check("RETRY")
}
