package e2e

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/kazuph/ikesu/internal/api"
)

func TestAgentAttachActualTerminal(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	var listed api.PaneListResult
	s.rpc(api.MethodPaneList, api.EmptyParams{}, &listed)
	pane := listed.Panes[0].PaneID
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: pane, Source: "test", Agent: "codex", State: "idle"}, nil)
	command := exec.Command(binPath, "--session", s.name, "agent", "attach", pane)
	command.Env = append(s.cmd.Env, "IKESU_STATE_DIR="+s.stateDir)
	pt, err := pty.StartWithSize(command, &pty.Winsize{Cols: termW, Rows: termH})
	if err != nil {
		t.Fatal(err)
	}
	defer pt.Close()
	e := vt.NewEmulator(termW, termH)
	defer e.Close()
	go io.Copy(io.Discard, e)
	chunks := make(chan []byte, 1024)
	go func() {
		defer close(chunks)
		b := make([]byte, 65536)
		for {
			n, err := pt.Read(b)
			if n > 0 {
				chunks <- append([]byte{}, b[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	finished := false
	t.Cleanup(func() {
		if !finished {
			_ = command.Process.Kill()
			<-exited
		}
	})
	screen := func() string {
		var b strings.Builder
		for y := 0; y < termH; y++ {
			for x := 0; x < termW; x++ {
				if c := e.CellAt(x, y); c != nil && c.Width > 0 {
					b.WriteString(c.Content)
				}
			}
			b.WriteByte('\n')
		}
		return b.String()
	}
	wait := func(label string, predicate func(string) bool) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case chunk, ok := <-chunks:
				if !ok {
					t.Fatalf("closed during %s", label)
				}
				e.Write(chunk)
				if predicate(screen()) {
					return
				}
			case <-deadline:
				t.Fatalf("%s: %s", label, screen())
			}
		}
	}
	wait("direct shell", func(text string) bool { return strings.Contains(text, "❯") })
	if _, err := pt.WriteString("printf 'DIRECT_%s\\n' SCREEN_OK\r"); err != nil {
		t.Fatal(err)
	}
	wait("executed output", func(text string) bool { return strings.Contains(text, "DIRECT_SCREEN_OK") })
	// The direct viewport contains no workspace/tab navigation from the full UI.
	if strings.Contains(screen(), "workspace 1") {
		t.Fatal("full UI leaked into direct terminal")
	}
	_, _ = pt.WriteString("for n in $(seq 1 80); do printf 'ROW_%03d\\n' $n; done\r")
	wait("last row", func(text string) bool { return strings.Contains(text, "ROW_080") })
	_, _ = pt.WriteString("\x1b[5~")
	wait("scrollback", func(text string) bool { return strings.Contains(text, "ROW_010") && !strings.Contains(text, "ROW_080") })
	_, _ = pt.WriteString("printf 'BACK_%s\\n' BOTTOM\r")
	wait("input returns to bottom", func(text string) bool {
		return strings.Contains(text, "BACK_BOTTOM") && strings.Contains(text, "ROW_080")
	})
	if err := pty.Setsize(pt, &pty.Winsize{Cols: termW, Rows: termH}); err != nil {
		t.Fatal(err)
	}
	_, _ = pt.WriteString("\x02q")
	select {
	case err := <-exited:
		finished = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("detach did not exit")
	}
	var pong api.PongResult
	s.rpc(api.MethodPing, api.EmptyParams{}, &pong)
	if pong.Type != "pong" {
		t.Fatal(fmt.Sprint(pong))
	}
}
