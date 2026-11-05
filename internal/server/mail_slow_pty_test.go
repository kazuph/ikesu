package server

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
)

// A real quiet interval and a filled PTY are necessary to exercise the retry.
func TestPendingMailSlowPTYDoesNotBlockApp(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "mail-slow-pty")
	s.mailCh = make(chan mailWrite, 8)
	s.mailInFlight = map[int64]bool{}
	s.jobInFlight = map[string]bool{}
	dir := t.TempDir()
	gate, capture, other := filepath.Join(dir, "gate"), filepath.Join(dir, "capture"), filepath.Join(dir, "other")
	if err := syscall.Mkfifo(gate, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := s.sup.SpawnWith(1, dir, 80, 24, nil, []string{"/bin/sh", "-c", fmt.Sprintf("stty raw -echo; printf READY; read line < %q; exec cat > %q", gate, capture)})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.sup.SpawnWith(2, dir, 80, 24, nil, []string{"/bin/sh", "-c", fmt.Sprintf("stty raw -echo; printf READY; exec cat > %q", other)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(p.ScreenText(), "READY") || !strings.Contains(p2.ScreenText(), "READY") {
		if time.Now().After(deadline) {
			t.Fatal("PTYs not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.app.SetReported(1, "codex", "working")
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	workerDone := make(chan struct{})
	released := false
	release := func() {
		if !released {
			f, e := os.OpenFile(gate, os.O_WRONLY, 0)
			if e == nil {
				_, _ = f.WriteString("go\n")
				_ = f.Close()
			}
			released = true
		}
	}
	defer func() { release(); close(s.mailCh); <-workerDone; s.app.Stop(); <-appDone }()
	first := bytes.Repeat([]byte("A"), 1024*1024)
	second := []byte("second-notice\r")
	if !s.enqueueMail(mailWrite{pane: 1, owner: p, payload: first}) || !s.enqueueMail(mailWrite{pane: 1, owner: p, payload: second}) {
		t.Fatal("enqueue failed")
	}
	p.RecordKeyInput()
	go func() { s.mailWorker(); close(workerDone) }()
	deadline = time.Now().Add(5 * time.Second)
	for {
		s.mailMu.Lock()
		_, held := s.mailDeferred[1]
		s.mailMu.Unlock()
		if held {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("mail not held")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(runtime.MailInputQuiet + 2*jobActiveRefresh)
	done := make(chan struct{})
	go func() { s.app.Do(func() { s.sup.Input(2, []byte("other-pane")) }); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("app loop stopped while PTY write blocked")
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(other)
		if string(b) == "other-pane" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("other pane input=%q", b)
		}
		time.Sleep(10 * time.Millisecond)
	}
	release()
	want := append(first, second...)
	deadline = time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(capture)
		if bytes.Equal(b, want) {
			break
		}
		if len(b) > len(want) || time.Now().After(deadline) {
			t.Fatalf("capture length=%d want=%d", len(b), len(want))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
