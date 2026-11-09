package runtime

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandoffCancelledBeforePauseKeepsPaneRunning(t *testing.T) {
	teststate.Lock(t)
	s := NewSupervisor()
	defer s.KillAll()
	p, err := s.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/sh", "-c", "read line; printf STILL_RUNNING; read line"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, files, err := s.PauseHandoffContext(ctx)
	for _, f := range files {
		f.Close()
	}
	if err == nil {
		s.ResumeHandoff()
		t.Fatal("cancelled pause succeeded")
	}
	s.Input(1, []byte("go\n"))
	until := time.After(2 * time.Second)
	for !strings.Contains(p.ScreenText(), "STILL_RUNNING") {
		select {
		case <-until:
			t.Fatal("pane did not resume after cancellation")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestHandoffPausesWithFullClipboardEventQueue(t *testing.T) {
	teststate.Lock(t)
	s := NewSupervisor()
	next := NewSupervisor()
	defer s.KillAll()
	defer next.KillAll()
	const count = 300
	script := fmt.Sprintf("import os\nos.write(1, b'\\x1b]52;c;eA==\\x07' * %d)\nos.read(0,1)\n", count)
	p, err := s.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/usr/bin/python3", "-c", script})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for len(s.events) < cap(s.events) {
		select {
		case <-deadline:
			t.Fatal("event queue did not fill")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	type result struct {
		states []PaneTransfer
		files  []*os.File
		err    error
	}
	resultCh := make(chan result, 1)
	go func() { states, files, err := s.PauseHandoff(); resultCh <- result{states, files, err} }()
	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if len(got.states) != 1 || len(got.states[0].Clipboard) != count {
			t.Fatalf("pending clipboard events in handoff: %d, want %d", len(got.states[0].Clipboard), count)
		}
		if err := next.ImportHandoff(got.states, got.files); err != nil {
			t.Fatal(err)
		}
		s.ReleaseHandoff()
		next.StartImported()
		for delivered := 0; delivered < count; {
			select {
			case event := <-next.Events():
				if event.Kind == EventClipboard {
					delivered++
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("received %d clipboard events, want %d", delivered, count)
			}
		}
	case <-time.After(time.Second):
		// Drain the real event queue so the old implementation can unwind.
		for len(s.events) > 0 {
			<-s.events
		}
		<-resultCh
		s.ResumeHandoff()
		t.Fatal("handoff waited for clipboard event receiver")
	}
	_ = p
}

func TestHandoffRollbackRedeliversClipboardEvents(t *testing.T) {
	teststate.Lock(t)
	s := NewSupervisor()
	defer s.KillAll()
	const count = 300
	script := fmt.Sprintf("import os\nos.write(1, b'\\x1b]52;c;eA==\\x07' * %d)\nos.read(0,1)\n", count)
	if _, err := s.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/usr/bin/python3", "-c", script}); err != nil {
		t.Fatal(err)
	}
	until := time.After(2 * time.Second)
	for len(s.events) < cap(s.events) {
		select {
		case <-until:
			t.Fatal("event queue did not fill")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	type paused struct {
		states []PaneTransfer
		files  []*os.File
		err    error
	}
	result := make(chan paused, 1)
	go func() { states, files, err := s.PauseHandoff(); result <- paused{states, files, err} }()
	var got paused
	select {
	case got = <-result:
	case <-time.After(time.Second):
		for len(s.events) > 0 {
			<-s.events
		}
		got = <-result
		for _, file := range got.files {
			file.Close()
		}
		s.ResumeHandoff()
		t.Fatal("handoff did not pause with a full event queue")
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	for _, file := range got.files {
		file.Close()
	}
	if len(got.states) != 1 || len(got.states[0].Clipboard) != count {
		t.Fatal("clipboard events were not captured")
	}
	s.ResumeHandoff()
	for delivered := 0; delivered < count; {
		select {
		case event := <-s.Events():
			if event.Kind == EventClipboard {
				delivered++
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("rollback delivered %d clipboard events, want %d", delivered, count)
		}
	}
}

func TestHandoffDeliversQueuedClipboardFromExitedPane(t *testing.T) {
	teststate.Lock(t)
	old, next := NewSupervisor(), NewSupervisor()
	defer old.KillAll()
	defer next.KillAll()
	script := "import os\nos.write(1, b'\\x1b]52;c;eA==\\x07' * 300)\n"
	p, err := old.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/usr/bin/python3", "-c", script})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Exited():
	case <-time.After(2 * time.Second):
		t.Fatal("pane did not exit")
	}
	states, files, err := old.PauseHandoff()
	if err != nil {
		t.Fatal(err)
	}
	count := len(states[0].Clipboard)
	if count == 0 || !states[0].Snapshot.Dead {
		t.Fatalf("queued events=%d dead=%v", count, states[0].Snapshot.Dead)
	}
	if err := next.ImportHandoff(states, files); err != nil {
		t.Fatal(err)
	}
	old.ReleaseHandoff()
	next.StartImported()
	for delivered := 0; delivered < count; {
		select {
		case event := <-next.Events():
			if event.Kind == EventClipboard {
				delivered++
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("import delivered %d of %d queued events", delivered, count)
		}
	}
}

func TestHandoffImportedPaneExitClosesExited(t *testing.T) {
	teststate.Lock(t)
	old, next := NewSupervisor(), NewSupervisor()
	defer old.KillAll()
	defer next.KillAll()
	_, err := old.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/sh", "-c", "read line"})
	if err != nil {
		t.Fatal(err)
	}
	states, files, err := old.PauseHandoff()
	if err != nil {
		t.Fatal(err)
	}
	if err := next.ImportHandoff(states, files); err != nil {
		t.Fatal(err)
	}
	old.ReleaseHandoff()
	next.StartImported()
	p := next.Get(1)
	select {
	case <-p.Exited():
		t.Fatal("pause closed Exited")
	default:
	}
	next.Input(1, []byte("exit\n"))
	select {
	case <-p.Exited():
	case <-time.After(2 * time.Second):
		t.Fatal("imported pane exit did not close Exited")
	}
	if !p.Snapshot().Dead {
		t.Fatal("exited pane is not dead")
	}
	for {
		select {
		case event := <-next.Events():
			if event.Kind == EventExited && event.Pane == p.ID {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("imported pane exit event missing")
		}
	}
}

func TestHandoffImportedPaneKillClosesExited(t *testing.T) {
	teststate.Lock(t)
	old, next := NewSupervisor(), NewSupervisor()
	defer old.KillAll()
	defer next.KillAll()
	if _, err := old.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/sh", "-c", "read line"}); err != nil {
		t.Fatal(err)
	}
	states, files, err := old.PauseHandoff()
	if err != nil {
		t.Fatal(err)
	}
	if err := next.ImportHandoff(states, files); err != nil {
		t.Fatal(err)
	}
	old.ReleaseHandoff()
	next.StartImported()
	p := next.Get(1)
	next.Kill(1)
	select {
	case <-p.Exited():
	case <-time.After(2 * time.Second):
		t.Fatal("killed imported pane did not close Exited")
	}
}

func TestHandoffActualPTYMouseAndExit(t *testing.T) {
	for _, tc := range []struct {
		name, modes string
		x, y        int
		want        []byte
	}{
		{"SGR cells", "\x1b[?1000h\x1b[?1006h", 3, 7, []byte("\x1b[<0;4;8M")},
		{"SGR pixels", "\x1b[?1002h\x1b[?1016h", 33, 70, []byte("\x1b[<0;34;71M")},
		{"X10", "\x1b[?1000h", 3, 7, []byte("\x1b[M $(")},
	} {
		t.Run(tc.name, func(t *testing.T) { testHandoffMouse(t, tc.modes, tc.x, tc.y, tc.want) })
	}
}

func TestHandoffActualPTYPartialParser(t *testing.T) {
	for _, tc := range []struct{ name, prefix, suffix string }{
		{"OSC title", "\x1b]2;HANDOFF_", "TITLE\a\x1b[2;4HTARGET"},
		{"CSI parameters", "\x1b[2;", "4HTARGET"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			teststate.Lock(t)
			s, next := NewSupervisor(), NewSupervisor()
			defer s.KillAll()
			defer next.KillAll()
			script := `import os,tty,sys
tty.setraw(0)
os.write(1,bytes.fromhex(sys.argv[1]))
os.read(0,1)
os.write(1,bytes.fromhex(sys.argv[2]))
os.read(0,1)
`
			probe := filepath.Join(t.TempDir(), "partial.py")
			if err := os.WriteFile(probe, []byte(script), 0600); err != nil {
				t.Fatal(err)
			}
			p, err := s.SpawnWith(1, "/tmp", 20, 6, nil, []string{"/usr/bin/python3", probe, hex.EncodeToString([]byte(tc.prefix)), hex.EncodeToString([]byte(tc.suffix))})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				p.mu.Lock()
				partial := p.read != nil && len(p.read.partial) > 0
				p.mu.Unlock()
				if partial {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("partial output not reached")
				}
				time.Sleep(time.Millisecond)
			}
			states, files, err := s.PauseHandoff()
			if err != nil {
				t.Fatal(err)
			}
			if err = next.ImportHandoff(states, files); err != nil {
				t.Fatal(err)
			}
			s.ReleaseHandoff()
			next.StartImported()
			q := next.Get(1)
			next.Input(1, []byte("g"))
			deadline = time.Now().Add(5 * time.Second)
			for {
				lines, _ := q.ReadLines("visible", true)
				if len(lines) > 1 && strings.HasPrefix(lines[1], "   TARGET") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("sequence not continued: %q", lines)
				}
				time.Sleep(time.Millisecond)
			}
			if tc.name == "OSC title" && q.Snapshot().OSCTitle != "HANDOFF_TITLE" {
				t.Fatal(q.Snapshot().OSCTitle)
			}
			t.Logf("PID=%d partial=%x resumed=%x", q.ShellPID(), tc.prefix, tc.suffix)
		})
	}
}

func testHandoffMouse(t *testing.T, modes string, x, y int, want []byte) {
	teststate.Lock(t)
	s := NewSupervisor()
	next := NewSupervisor()
	defer s.KillAll()
	defer next.KillAll()
	path := filepath.Join(t.TempDir(), "mouse")
	script := `import os,tty,sys
f=open(sys.argv[1],"wb",buffering=0)
tty.setraw(0)
os.write(1,sys.argv[2].encode()+b"MOUSE_READY")
while True:
 b=os.read(0,1024)
 if b==b"x":break
 f.write(b)
`
	probe := filepath.Join(t.TempDir(), "probe.py")
	if err := os.WriteFile(probe, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := s.SpawnWith(1, "/tmp", 80, 24, nil, []string{"/usr/bin/python3", probe, path, modes})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(fn func() bool) {
		t.Helper()
		end := time.Now().Add(5 * time.Second)
		for time.Now().Before(end) {
			if fn() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("PTY condition not reached")
	}
	wait(func() bool { return p.Snapshot().MouseTracking })
	click := func(p *Pane) {
		p.WithEmu(func(e *vt.Emulator) { e.SendMouse(uv.MouseClickEvent(uv.Mouse{X: x, Y: y, Button: uv.MouseLeft})) })
	}
	click(p)
	wait(func() bool { b, _ := os.ReadFile(path); return bytes.Equal(b, want) })
	states, files, err := s.PauseHandoff()
	if err != nil {
		t.Fatal(err)
	}
	if err = next.ImportHandoff(states, files); err != nil {
		t.Fatal(err)
	}
	s.ReleaseHandoff()
	next.StartImported()
	q := next.Get(1)
	if q.ShellPID() != p.ShellPID() {
		t.Fatal("PID changed")
	}
	click(q)
	wait(func() bool {
		b, _ := os.ReadFile(path)
		return bytes.Equal(b, append(append([]byte{}, want...), want...))
	})
	t.Logf("PID=%d mouse before/after=%x", q.ShellPID(), want)
	next.Input(1, []byte("x"))
	wait(func() bool { return q.Snapshot().Dead })
}
