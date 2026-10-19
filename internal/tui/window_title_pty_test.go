package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

// Exercise the standalone draw path with real shell PTYs and an outer PTY.
// State changes use the production reporting/acknowledgment methods.
func TestWindowTitleStandaloneActualPTYTransitions(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	chunks := make(chan []byte, 64)
	go func() {
		defer close(chunks)
		b := make([]byte, 32768)
		for {
			n, err := master.Read(b)
			if n > 0 {
				chunks <- append([]byte(nil), b[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	a.out = slave
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.newWorkspace()
	pid := a.focusedPane()
	a.exec(model.CmdSelectWorkspace{Index: 0})
	var raw strings.Builder
	paint := func(want string) {
		t.Helper()
		a.draw()
		var received strings.Builder
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for !strings.Contains(received.String(), "\x1b]0;"+want+"\x07") {
			select {
			case b, ok := <-chunks:
				if !ok {
					t.Fatal(io.EOF)
				}
				received.Write(b)
				raw.Write(b)
			case <-timer.C:
				t.Fatalf("missing title %q, received %q", want, received.String())
			}
		}
		t.Logf("outer PTY title: %s", want)
	}
	a.SetReported(pid, "codex", "working")
	paint("🟡 gtx")
	paint("🟡 gtx") // second activation paint
	a.SetReported(pid, "codex", "blocked")
	paint("🔴 gtx")
	a.SetReported(pid, "codex", "idle")
	paint("🔵 gtx")
	a.exec(model.CmdSelectWorkspace{Index: 1})
	a.detectAll()
	paint("gtx")
	if out := os.Getenv("GTX_TITLE_EVIDENCE"); out != "" {
		if err := os.MkdirAll(out, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "standalone.raw"), []byte(raw.String()), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
