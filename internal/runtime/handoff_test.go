package runtime

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHandoffActualPTYMouseAndExit(t *testing.T) {
	teststate.Lock(t)
	s := NewSupervisor()
	next := NewSupervisor()
	defer s.KillAll()
	defer next.KillAll()
	path := filepath.Join(t.TempDir(), "mouse")
	script := `import os,tty,sys
f=open(sys.argv[1],"wb",buffering=0)
tty.setraw(0)
os.write(1,b"\x1b[?1000h\x1b[?1006hMOUSE_READY")
while True:
 b=os.read(0,1024)
 if b==b"x":break
 f.write(b)
`
	probe := filepath.Join(t.TempDir(), "probe.py")
	if err := os.WriteFile(probe, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := s.SpawnWith(1, "/tmp", 80, 24, nil, []string{"/usr/bin/python3", probe, path})
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
		p.WithEmu(func(e *vt.Emulator) { e.SendMouse(uv.MouseClickEvent(uv.Mouse{X: 3, Y: 7, Button: uv.MouseLeft})) })
	}
	click(p)
	want := []byte("\x1b[<0;4;8M")
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
