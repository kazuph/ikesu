package e2e

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/kazuph/ikesu/internal/api"
)

type sizeGeometry struct{ Rows, Cols, X, Y, PID int }

func (s *session) observedPTYGeometry(pid string) sizeGeometry {
	s.t.Helper()
	path := filepath.Join(s.t.TempDir(), "geometry")
	// Both stty and ioctl run inside the existing pane process tree.
	command := fmt.Sprintf("stty size > '%s'; /usr/bin/python3 -c 'import fcntl,termios,struct; print(*struct.unpack(\"HHHH\",fcntl.ioctl(0,termios.TIOCGWINSZ,bytes(8))))' >> '%s'; printf '%%s\\n' $$ >> '%s'", path, path, path)
	s.rpc(api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: pid, Text: command, Keys: []string{"enter"}}, nil)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(path)
		var rows, cols int
		var g sizeGeometry
		n, err := fmt.Sscan(string(data), &rows, &cols, &g.Rows, &g.Cols, &g.X, &g.Y, &g.PID)
		if err == nil && n == 7 {
			if rows != g.Rows || cols != g.Cols {
				s.t.Fatal("stty and ioctl disagree", string(data))
			}
			return g
		}
		s.pump(time.Now().Add(10 * time.Millisecond))
	}
	data, _ := os.ReadFile(path)
	s.t.Fatalf("no pane PTY measurement: %q", data)
	return sizeGeometry{}
}

func (s *session) waitPTYGeometry(pid string, ready func(sizeGeometry) bool) sizeGeometry {
	s.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var g sizeGeometry
	for time.Now().Before(deadline) {
		g = s.observedPTYGeometry(pid)
		if ready(g) {
			return g
		}
		s.pump(time.Now().Add(10 * time.Millisecond))
	}
	s.t.Fatal("client geometry did not arrive", g)
	return g
}

func sizeClient(t *testing.T, s *session) (*os.File, func()) {
	t.Helper()
	cmd := exec.Command(binPath, "--session", s.name)
	cmd.Env = s.cmd.Env
	pt, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 82, Rows: 30, X: 82 * 10, Y: 30 * 20})
	if err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, pt)
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		if _, err := pt.WriteString("\x02q"); err != nil {
			t.Error(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("isolated size client did not detach", cmd.Process.Pid)
		}
		pt.Close()
	}
	t.Cleanup(stop)
	return pt, stop
}

func TestMultiClientFrozenHiddenPTYAcrossDetachAndLiveHandoff(t *testing.T) {
	t.Parallel()
	for _, handoff := range []bool{false, true} {
		t.Run(fmt.Sprint(handoff), func(t *testing.T) {
			s := start(t)
			s.waitFor("initial shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
			if err := pty.Setsize(s.pt, &pty.Winsize{Cols: 196, Rows: 50, X: 196 * 8, Y: 50 * 16}); err != nil {
				t.Fatal(err)
			}
			s.send("\x1b[<35;40;10M")
			hidden := "p1"
			before := s.waitPTYGeometry(hidden, func(g sizeGeometry) bool { return g.Rows == 46 && g.X == g.Cols*8 && g.Y == g.Rows*16 })
			if before.Cols >= 196 || before.Rows != 46 || before.X != before.Cols*8 || before.Y != before.Rows*16 {
				t.Fatal("large display geometry not established", before)
			}
			var tab api.TabCreatedResult
			s.rpc(api.MethodTabCreate, api.TabCreateParams{Focus: true}, &tab)
			client, stop := sizeClient(t, s)
			if _, err := client.WriteString("\x1b[<35;40;10M"); err != nil {
				t.Fatal(err)
			}
			small := s.waitPTYGeometry(tab.RootPane.PaneID, func(g sizeGeometry) bool { return g.Rows == 26 && g.X == g.Cols*10 && g.Y == g.Rows*20 })
			if small.Rows != 26 || small.Cols >= 82 || small.X != small.Cols*10 || small.Y != small.Rows*20 {
				t.Fatal("small display geometry not established", small)
			}
			s.send("\x02q")
			if err := s.cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			if handoff {
				if out, err := s.cli("server", "live-handoff"); err != nil {
					t.Fatalf("live handoff: %v %s", err, out)
				}
			} else {
				stop()
				client, stop = sizeClient(t, s)
			}
			if _, err := client.WriteString("\x1b[<35;40;10M"); err != nil {
				t.Fatal(err)
			}
			// A measurement on U orders the client activity before the non-focused API split.
			current := s.waitPTYGeometry(tab.RootPane.PaneID, func(g sizeGeometry) bool { return g.Rows == small.Rows && g.Cols == small.Cols })
			if current.Rows != small.Rows || current.Cols != small.Cols {
				t.Fatal("reconnected view is not on U", current, small)
			}
			target := tab.RootPane.PaneID
			s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &target, Direction: "right", Focus: false}, nil)
			after := s.observedPTYGeometry(hidden)
			if after != before {
				t.Fatal("hidden T changed without selection", before, after)
			}
			split := s.observedPTYGeometry(target)
			if split.Cols != (small.Cols-1)/2 || split.Rows != small.Rows || split.PID != small.PID {
				t.Fatal("U split geometry or process changed", small, split)
			}
			if _, err := client.WriteString("\x021"); err != nil {
				t.Fatal(err)
			}
			selected := s.waitPTYGeometry(hidden, func(g sizeGeometry) bool { return g.Rows == small.Rows && g.Cols == small.Cols })
			if selected.Rows != small.Rows || selected.Cols != small.Cols || selected.X != small.X || selected.Y != small.Y || selected.PID != before.PID {
				t.Fatal("T did not follow B on selection", before, selected, small)
			}
			t.Logf("handoff=%t frozen T=%+v split U=%+v selected T=%+v", handoff, before, split, selected)
			stop()
		})
	}
}
