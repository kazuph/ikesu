package runtime

import (
	"bytes"
	"encoding/hex"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
