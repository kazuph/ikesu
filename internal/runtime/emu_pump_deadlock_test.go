package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/teststate"
)

// 2026-11-09: an application that enabled mouse reporting but stopped reading
// its input froze the whole server. Wheel events were written into the
// emulator's response pipe under p.mu; the pipe's reader was stuck writing a
// full PTY; and the screen-output pump waited for p.mu. Mouse input to such a
// pane must never block, and its screen output must keep flowing.
func TestMouseToAnAppThatStopsReadingDoesNotDeadlock(t *testing.T) {
	teststate.Lock(t)
	script := filepath.Join(t.TempDir(), "noread.sh")
	body := "#!/bin/sh\nstty raw -echo\nprintf '\\033[?1000h\\033[?1006h'\ni=0\nwhile :; do i=$((i+1)); echo \"tick $i\"; done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", script)
	s := NewSupervisor()
	p, err := s.Spawn(1, "/tmp", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.KillAll()
	deadline := time.Now().Add(5 * time.Second)
	for !mouseOn(p) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !mouseOn(p) {
		t.Fatal("application never enabled mouse reporting")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		// About 600 KB of encoded wheel events: far beyond a PTY input buffer.
		for i := 0; i < 50000; i++ {
			p.SendMouse(uv.MouseWheelEvent{X: 1, Y: 1, Button: uv.MouseWheelDown})
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("SendMouse blocked on an application that does not read its input")
	}
	before := p.ScreenText()
	changed := false
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if p.ScreenText() != before {
			changed = true
			break
		}
	}
	if !changed {
		t.Fatal("screen output stopped flowing after mouse input")
	}
}

func mouseOn(p *Pane) bool {
	on := false
	p.WithEmu(func(e *vt.Emulator) { on = e.ModeSet(ansi.ModeMouseNormal) })
	return on
}

func spawnNonReader(t *testing.T, loop string) (*Supervisor, *Pane) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "noread.sh")
	body := "#!/bin/sh\nstty raw -echo\nprintf '\\033[?1000h\\033[?1006h'\n" + loop + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", script)
	s := NewSupervisor()
	p, err := s.Spawn(1, "/tmp", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.KillAll)
	for end := time.Now().Add(5 * time.Second); !mouseOn(p) && time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
	}
	return s, p
}

func within(t *testing.T, what string, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s blocked on an application that does not read its input", what)
	}
}

// Terminal queries (cursor position reports) answered by the emulator while
// the screen-output pump holds p.mu must not block either.
func TestTerminalQueriesFromAnAppThatStopsReadingDoNotDeadlock(t *testing.T) {
	teststate.Lock(t)
	_, p := spawnNonReader(t, "while :; do printf '\\033[6n.'; done")
	within(t, "screen access", 10*time.Second, func() {
		for i := 0; i < 200; i++ {
			_ = p.ScreenText()
		}
	})
}

// Keys and pastes are queued; the app loop never waits for the PTY, and a
// pane that stopped reading refuses input beyond the limit instead.
func TestKeyInputToAnAppThatStopsReadingReturns(t *testing.T) {
	teststate.Lock(t)
	s, p := spawnNonReader(t, "exec sleep 600")
	chunk := make([]byte, 64<<10)
	for i := range chunk {
		chunk[i] = 'x'
	}
	within(t, "key input", 10*time.Second, func() {
		for i := 0; i < 200; i++ { // 12.5 MiB, beyond ptyInputLimit
			s.Input(p.ID, chunk)
		}
	})
	if err := p.QueuePTY(chunk); err != ErrPTYInputFull {
		t.Fatalf("queue beyond the limit: %v", err)
	}
}

// Mail delivery waits for the PTY without holding the input lock, so the app
// loop's key recording is never stuck behind it.
func TestMailWaitingOnAFullPTYDoesNotHoldTheInputLock(t *testing.T) {
	teststate.Lock(t)
	_, p := spawnNonReader(t, "exec sleep 600")
	filler := make([]byte, 1<<20)
	for i := range filler {
		filler[i] = 'y'
	}
	go func() { _, _ = p.WriteMail(filler) }()
	time.Sleep(200 * time.Millisecond)
	within(t, "key recording during mail delivery", 5*time.Second, func() {
		p.RecordKeyInput()
		_ = p.MailInputActive()
	})
}
