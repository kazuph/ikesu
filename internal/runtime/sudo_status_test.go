package runtime

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"os/exec"
	"os/user"
	"testing"
	"time"
)

func TestForegroundSudoRestoresOnInterrupt(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	if err := exec.Command("/usr/bin/sudo", "-k").Run(); err != nil {
		t.Fatal(err)
	}
	s := NewSupervisor()
	defer s.KillAll()
	p, err := s.Spawn(1, t.TempDir(), 100, 24)
	if err != nil {
		t.Fatal(err)
	}
	wait := func(active bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			p.PollProcess()
			if p.ForegroundSudo() == active {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("sudo=%v did not converge: %v", active, p.ScreenText())
			}
			time.Sleep(time.Millisecond)
		}
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.WriteToPTY([]byte("/usr/bin/sudo -n -u " + u.Username + " /bin/sleep 60\n")); err != nil {
		t.Fatal(err)
	}
	wait(true)
	readAt := p.processCache.readAt
	for i := 0; i < 10; i++ {
		if !p.ForegroundSudo() {
			t.Fatal("sudo disappeared")
		}
	}
	if !p.processCache.readAt.Equal(readAt) {
		t.Fatal("reading sudo status repeated process scan")
	}
	if _, err := p.WriteToPTY([]byte{3}); err != nil {
		t.Fatal(err)
	}
	wait(false)

	if _, err := p.WriteToPTY([]byte("exec /usr/bin/sudo -n -u " + u.Username + " /bin/sleep 60\n")); err != nil {
		t.Fatal(err)
	}
	wait(true)
	if _, err := p.WriteToPTY([]byte{3}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !p.Snapshot().Dead {
		if time.Now().After(deadline) {
			t.Fatal("exec sudo pane did not exit")
		}
		time.Sleep(time.Millisecond)
	}
	// No new process scan is needed to remove the overlay from a dead pane.
	if p.ForegroundSudo() {
		t.Fatal("sudo overlay survived pane exit")
	}

}
