package runtime

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIntegratedLoginRecoveryThenSudoOverlayClears(t *testing.T) {
	teststate.Lock(t)
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	shell := filepath.Join(dir, "login-shell")
	script := "#!/bin/sh\nif [ ! -f '" + ready + "' ]; then exit 1; fi\nprintf '\\000/usr/bin:/bin\\000'\n"
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	s := NewSupervisor()
	defer s.KillAll()
	first := s.WaitLoginPath()
	if first == nil {
		t.Fatal("failed login lookup unexpectedly succeeded")
	}
	if err := os.WriteFile(ready, []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitLoginPath(); err != first {
		t.Fatal("failure cooldown changed", err)
	}
	time.Sleep(loginPathFailureCooldown + time.Millisecond)
	if err := s.WaitLoginPath(); err != nil {
		t.Fatal("login PATH did not recover", err)
	}
	p, err := s.SpawnAgent(1, dir, 100, 24, nil, []string{"/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(name string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			p.PollProcess()
			if condition() {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal(name, p.ScreenText())
	}
	if _, err = p.WriteToPTY([]byte("printf 'RECOVERED_%s\\n' READY\n")); err != nil {
		t.Fatal(err)
	}
	wait("agent shell after PATH recovery", func() bool { return strings.Contains(p.ScreenText(), "RECOVERED_READY") })
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.WriteToPTY([]byte("/usr/bin/sudo -n -u " + current.Username + " /bin/sleep 60\n")); err != nil {
		t.Fatal(err)
	}
	wait("foreground sudo", p.ForegroundSudo)
	if _, err = p.WriteToPTY([]byte{3}); err != nil {
		t.Fatal(err)
	}
	wait("sudo overlay cleared after interrupt", func() bool { return !p.ForegroundSudo() })
	if got, err := s.LoginPath(); err != nil || got != "/usr/bin:/bin" {
		t.Fatal("recovered PATH cache changed", got, err)
	}
	t.Log("real login failure -> cooldown -> recovery -> agent PTY -> sudo active -> interrupt -> overlay cleared")
}
