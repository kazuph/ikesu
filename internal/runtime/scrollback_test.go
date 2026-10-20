package runtime

import (
	"github.com/kazuph/ikesu/internal/teststate"
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

func TestScrollbackRetainsWideLineEndFromPTY(t *testing.T) {
	teststate.Lock(t)
	s := NewSupervisor()
	p, err := s.SpawnWith(1, t.TempDir(), 60, 5, nil, []string{"/bin/sh", "-c", "i=0; while [ $i -lt 20 ]; do printf '日本語あいう\\n'; i=$((i+1)); done; printf DONE; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.KillAll()
	deadline := time.Now().Add(3 * time.Second)
	for p.Snapshot().ScrollbackLen < 10 {
		if time.Now().After(deadline) {
			t.Fatal("real PTY did not produce scrollback")
		}
		time.Sleep(10 * time.Millisecond)
	}
	row := p.BufferLines()[0]
	if row[10].Content != "う" || row[10].Width != 2 || row[11].Width != 0 {
		t.Fatalf("wide line end lost its continuation: %+v %+v", row[10], row[11])
	}
}
