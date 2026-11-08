package server

import (
	"os"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestIdleActivitySessionRestore(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "idle-activity")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	id := s.app.State().FocusedPane()
	p := s.sup.Get(id)
	at := time.Now().Add(-time.Hour).UTC()
	p.RestoreIdleActivity(at, 8)
	snapshot := s.captureSessionWithoutObservation()
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := session.Save(session.Path(s.dir), snapshot.file); err != nil {
		t.Fatal(err)
	}
	f, err := session.Load(session.Path(s.dir))
	if err != nil {
		t.Fatal(err)
	}
	r := newTestServer(t, "idle-restore")
	defer r.sup.KillAll()
	r.app.RestoreSession(f)
	got, seq := r.sup.Get(id).IdleActivity()
	if !got.Equal(at) || seq != 8 {
		t.Fatalf("restore = %v %d, want %v 8", got, seq, at)
	}
}
