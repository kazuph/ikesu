package runtime

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
	"time"
)

func TestScrollbackGrows(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	s := NewSupervisor()
	p, err := s.Spawn(1, t.TempDir(), 60, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer s.KillAll()
	// wait for shell to be ready
	time.Sleep(800 * time.Millisecond)
	s.Input(1, []byte("seq 1 80\n"))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p.Snapshot().ScrollbackLen > 10 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("scrollback=%d after seq", p.Snapshot().ScrollbackLen)
}
