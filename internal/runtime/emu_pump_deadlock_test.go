package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
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
		// About 60 KB of encoded wheel events: far beyond a PTY input buffer.
		for i := 0; i < 5000; i++ {
			p.SendMouse(uv.MouseWheelEvent{X: 1, Y: 1, Button: uv.MouseWheelDown})
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second): // output batches hold p.mu for seconds under -race
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
	if !mouseOn(p) {
		t.Fatalf("test application never started: %q", p.ScreenText())
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
		buf := make([]byte, 1<<20)
		n := goruntime.Stack(buf, true)
		t.Fatalf("%s blocked on an application that does not read its input\n%s", what, buf[:n])
	}
}

// Terminal queries (cursor position reports) answered by the emulator while
// the screen-output pump holds p.mu must not block either. The test first
// waits until the application's input is demonstrably not being read.
func TestTerminalQueriesFromAnAppThatStopsReadingDoNotDeadlock(t *testing.T) {
	teststate.Lock(t)
	_, p := spawnNonReader(t, "while :; do printf '\\033[6n.'; done")
	waitQueued(t, p, 64<<10)
	within(t, "screen access with a full PTY", 30*time.Second, func() {
		for i := 0; i < 5; i++ {
			_ = p.ScreenText()
		}
	})
}

// waitQueued waits until at least n accepted bytes are waiting for the
// application, which proves the PTY input is full (the old deadlock state).
func waitQueued(t *testing.T, p *Pane, n int) {
	t.Helper()
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if queuedBytes(p) >= n {
			return
		}
	}
	t.Fatalf("PTY input never filled: %d bytes queued", queuedBytes(p))
}

func queuedBytes(p *Pane) int {
	p.writer.mu.Lock()
	defer p.writer.mu.Unlock()
	return p.writer.queued
}

func queuedTail(p *Pane, n int) string {
	p.writer.mu.Lock()
	defer p.writer.mu.Unlock()
	var q []byte
	for _, s := range p.writer.segments {
		q = append(q, s.data...)
	}
	return string(q[max(0, len(q)-n):])
}

func fillQueue(t *testing.T, p *Pane, n int) {
	t.Helper()
	filler := make([]byte, n)
	for i := range filler {
		filler[i] = 'f'
	}
	if err := p.QueuePTY(filler); err != nil {
		t.Fatal(err)
	}
	waitQueued(t, p, min(n, 64<<10))
}

// A paste is accepted or refused whole, so a refused paste never leaves an
// unmatched bracketed-paste start marker behind.
func TestPasteIsAcceptedOrRefusedWhole(t *testing.T) {
	teststate.Lock(t)
	_, p := spawnNonReader(t, "printf '\\033[?2004h'; exec sleep 600")
	for end := time.Now().Add(5 * time.Second); !p.BracketedPasteMode() && time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
	}
	if !p.BracketedPasteMode() {
		t.Fatal("bracketed paste never enabled")
	}
	fillQueue(t, p, 256<<10)
	if err := p.QueuePTY(make([]byte, ptyInputLimit-queuedBytes(p)-8)); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("p", 4096)
	before := queuedBytes(p)
	p.WithEmu(func(e *vt.Emulator) { e.Paste(body) })
	if queuedBytes(p) > before {
		t.Fatalf("a paste larger than the free space was accepted in part: %d -> %d", before, queuedBytes(p))
	}
	if tail := queuedTail(p, 8192); strings.Contains(tail, "\x1b[200~") {
		t.Fatalf("a refused paste left its start marker: %q", tail[len(tail)-64:])
	}
}

// Input encoded by the emulator keeps its place relative to keys sent after it.
func TestEmulatorInputKeepsOrderWithLaterKeys(t *testing.T) {
	teststate.Lock(t)
	s, p := spawnNonReader(t, "exec sleep 600")
	fillQueue(t, p, 256<<10)
	want := ""
	for i := 0; i < 200; i++ {
		p.WithEmu(func(e *vt.Emulator) { e.Paste(fmt.Sprintf("<%d>", i)) })
		s.Input(p.ID, []byte(fmt.Sprintf("[%d]", i)))
		want += fmt.Sprintf("<%d>[%d]", i, i)
	}
	if got := queuedTail(p, len(want)); got != want {
		t.Fatalf("emulator input and keys reordered:\n got %q\nwant %q", got[:min(80, len(got))], want[:80])
	}
}

// spawnLateReader starts an application that ignores its input for a while
// and then copies everything it reads into a file.
func spawnLateReader(t *testing.T, delay string) (*Supervisor, *Pane, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "received")
	s, p := spawnNonReader(t, "sleep "+delay+"; exec cat > "+out)
	return s, p, out
}

func waitFileSuffix(t *testing.T, path, suffix string) string {
	t.Helper()
	var got []byte
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		got, _ = os.ReadFile(path)
		if strings.HasSuffix(string(got), suffix) {
			return string(got)
		}
	}
	t.Fatalf("application never received %q; got %d bytes ending %q", suffix, len(got), string(got[max(0, len(got)-40):]))
	return ""
}

// A live handoff carries every accepted but unwritten byte to the next
// server, including input accepted after the pause, and the next server
// writes it first without waiting for more input (Astra F3, F4).
func TestHandoffCarriesAcceptedInputToTheNextServer(t *testing.T) {
	teststate.Lock(t)
	old, p, received := spawnLateReader(t, "2")
	filler := strings.Repeat("f", 128<<10)
	if err := p.QueuePTY([]byte(filler)); err != nil {
		t.Fatal(err)
	}
	old.Input(p.ID, []byte("A"))
	states, files, err := old.PauseHandoff()
	if err != nil {
		t.Fatal(err)
	}
	old.Input(p.ID, []byte("B")) // accepted after the pause, before the next server runs
	next := NewSupervisor()
	defer next.KillAll()
	if err := next.ImportHandoff(states, files); err != nil {
		t.Fatal(err)
	}
	next.StartImported()
	pending := old.TakePendingInputs()
	old.Input(p.ID, []byte("X")) // refused: this server no longer owns the pane's input
	next.RestorePendingInputs(pending)
	next.Input(p.ID, []byte("C"))
	old.ReleaseHandoff()
	got := waitFileSuffix(t, received, "ABC")
	if strings.Count(got, "A") != 1 || strings.Contains(got, "X") || !strings.HasSuffix(got, filler+"ABC") {
		t.Fatalf("input lost, duplicated or reordered across the handoff: %d bytes ending %q", len(got), got[max(0, len(got)-20):])
	}
}

// A refused handoff resumes writing the queued input.
func TestRefusedHandoffKeepsWriting(t *testing.T) {
	teststate.Lock(t)
	s, p, received := spawnLateReader(t, "1")
	s.Input(p.ID, []byte("A"))
	if _, files, err := s.PauseHandoff(); err != nil {
		t.Fatal(err)
	} else {
		for _, f := range files {
			f.Close()
		}
	}
	s.Input(p.ID, []byte("B"))
	s.ResumeHandoff()
	s.Input(p.ID, []byte("C"))
	waitFileSuffix(t, received, "ABC")
}

// Screen-prompt keys accepted while the PTY is full are dropped, not sent
// late, when external input arrives or their deadline passes (Astra F1).
func TestStaleScreenPromptKeysAreNotSentLate(t *testing.T) {
	teststate.Lock(t)
	s, p, received := spawnLateReader(t, "1")
	seq := p.ExternalInputSequence()
	if _, err := p.WriteScreenPrompt([]byte("STALE1"), &seq, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.WriteScreenPrompt([]byte("STALE2"), nil, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	s.Input(p.ID, []byte("HUMAN")) // external input after the first answer was accepted
	got := waitFileSuffix(t, received, "HUMAN")
	if strings.Contains(got, "STALE") {
		t.Fatalf("stale screen keys were written: %q", got)
	}
}

// Mail is reported written only after its bytes reached the PTY (Astra F2).
func TestMailCompletionWaitsForThePTY(t *testing.T) {
	teststate.Lock(t)
	_, p, received := spawnLateReader(t, "1")
	if err := p.QueuePTY([]byte(strings.Repeat("f", 128<<10))); err != nil {
		t.Fatal(err)
	}
	done, err := p.WriteMail([]byte("MAIL\r"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("mail reported written while the application was not reading: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("mail never completed")
	}
	waitFileSuffix(t, received, "MAIL\r")
}

// Mail queued for a pane whose process exits is reported not written.
func TestMailToAnExitedPaneIsNotReportedWritten(t *testing.T) {
	teststate.Lock(t)
	_, p := spawnNonReader(t, "sleep 1; exit 0")
	if err := p.QueuePTY([]byte(strings.Repeat("f", 128<<10))); err != nil {
		t.Fatal(err)
	}
	done, err := p.WriteMail([]byte("MAIL\r"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("mail to an exited pane was reported written")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("mail to an exited pane never finished")
	}
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

// Mail is accepted in order without waiting for the application, so mail
// delivery never holds its locks while a PTY is full.
func TestMailToAFullPTYReturnsAtOnce(t *testing.T) {
	teststate.Lock(t)
	_, p := spawnNonReader(t, "exec sleep 600")
	fillQueue(t, p, 256<<10)
	within(t, "mail to a full PTY", 5*time.Second, func() {
		if _, err := p.WriteMail([]byte("mail body\r")); err != nil {
			t.Errorf("mail not accepted: %v", err)
		}
	})
	if got := queuedTail(p, 10); got != "mail body\r" {
		t.Fatalf("mail not queued in order: %q", got)
	}
	within(t, "key recording after mail", 5*time.Second, func() {
		p.RecordKeyInput()
		_ = p.MailInputActive()
	})
}

// After the pane's process exits the PTY write fails; the emulator's
// response pipe must still be drained so later mouse input and pastes return.
func TestMouseAfterTheProcessExitsDoesNotBlock(t *testing.T) {
	teststate.Lock(t)
	s, p := spawnNonReader(t, "sleep 0.3; exit 0")
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit")
	}
	within(t, "mouse and paste after exit", 5*time.Second, func() {
		for i := 0; i < 2000; i++ {
			p.SendMouse(uv.MouseWheelEvent{X: 1, Y: 1, Button: uv.MouseWheelDown})
		}
		p.WithEmu(func(e *vt.Emulator) { e.Paste("hello") })
		p.WithEmu(func(e *vt.Emulator) { e.Paste("hello") })
	})
	within(t, "kill after exit", 5*time.Second, func() { s.Kill(p.ID) })
}
