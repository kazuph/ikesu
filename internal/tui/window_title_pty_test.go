package tui

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

// Keep the opt-in output path before TestMain isolates GTX_* defaults.
var titleEvidenceDir = os.Getenv("GTX_TITLE_EVIDENCE")

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
	a.windowTitleSession = "coding"
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
	paint("🟡 ikesu:coding")
	paint("🟡 ikesu:coding") // second activation paint
	cfg := a.sidebarConfig
	cfg.UI.WindowTitle = "plain"
	a.ApplyReloadedConfig(cfg)
	paint("ikesu:coding")
	var unchanged bytes.Buffer
	a.out = &unchanged
	a.ApplyReloadedConfig(cfg)
	a.draw()
	if strings.Contains(unchanged.String(), "\x1b]0;") {
		t.Fatal("unchanged plain config resent title")
	}
	a.out = slave
	cfg.UI.WindowTitle = "summary"
	a.ApplyReloadedConfig(cfg)
	paint("🟡 ikesu:coding")
	a.SetReported(pid, "codex", "blocked")
	paint("🔴 ikesu:coding")
	a.SetReported(pid, "codex", "idle")
	paint("🔵 ikesu:coding")
	a.exec(model.CmdSelectWorkspace{Index: 1})
	a.detectAll()
	paint("ikesu:coding")
	if out := titleEvidenceDir; out != "" {
		if err := os.MkdirAll(out, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "standalone.raw"), []byte(raw.String()), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
