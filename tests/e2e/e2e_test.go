// Package e2e boots the real gtx binary on a PTY and drives it with raw
// input bytes, reading the screen through a vt.Emulator — the same things a
// user terminal does.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

const (
	termW = 120
	termH = 40
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gtx-e2e")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	binPath = filepath.Join(dir, "gtx")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/gtx")
	cmd.Dir = repoRoot()
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic("build gtx: " + err.Error())
	}
	os.Exit(m.Run())
}

func repoRoot() string {
	wd, _ := os.Getwd()
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// session is a running gtx under a PTY with its screen decoded to an
// emulator.
type session struct {
	t   *testing.T
	pt  *os.File
	cmd *exec.Cmd
	emu *vt.Emulator
	buf *lockedBuf
}

type lockedBuf struct {
	ch chan []byte
}

func start(t *testing.T) *session {
	t.Helper()
	// Deterministic shell inside panes: a bare ZDOTDIR with a fixed PS1 so
	// prompt waits do not depend on the host's zshrc/direnv.
	zdot := t.TempDir()
	if err := os.WriteFile(filepath.Join(zdot, ".zshrc"), []byte("PS1='❯ '\n"), 0o644); err != nil {
		t.Fatalf("zshrc: %v", err)
	}
	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"SHELL=/bin/zsh",
		"ZDOTDIR="+zdot,
	)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: termH, Cols: termW})
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}
	s := &session{t: t, pt: f, cmd: cmd, emu: vt.NewEmulator(termW, termH), buf: &lockedBuf{ch: make(chan []byte, 4096)}}
	go func() {
		b := make([]byte, 64*1024)
		for {
			n, err := f.Read(b)
			if n > 0 {
				c := make([]byte, n)
				copy(c, b[:n])
				s.buf.ch <- c
			}
			if err != nil {
				close(s.buf.ch)
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = f.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	return s
}

// pump drains the PTY into the emulator until the deadline.
func (s *session) pump(deadline time.Time) {
	for time.Now().Before(deadline) {
		select {
		case d, ok := <-s.buf.ch:
			if !ok {
				return
			}
			_, _ = s.emu.Write(d)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// waitFor polls the screen until pred holds or timeout.
func (s *session) waitFor(what string, timeout time.Duration, pred func(string) bool) {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.pump(time.Now().Add(100 * time.Millisecond))
		if pred(s.text()) {
			return
		}
	}
	s.t.Fatalf("timed out waiting for %s; screen:\n%s", what, s.text())
}

func (s *session) text() string {
	var b strings.Builder
	for y := 0; y < termH; y++ {
		var sb strings.Builder
		for x := 0; x < termW; x++ {
			if c := s.emu.CellAt(x, y); c != nil && c.Width > 0 {
				sb.WriteString(c.Content)
			}
		}
		if y > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(sb.String(), " "))
	}
	return b.String()
}

// find returns the screen position (0-based cell coords) of text, or -1,-1.
// Byte offsets must not be used: box-drawing runes are multi-byte.
func (s *session) find(text string) (int, int) {
	for y := 0; y < termH; y++ {
		var row strings.Builder
		cellAt := make([]int, 0, termW) // byte offset where each column starts
		for x := 0; x < termW; x++ {
			cellAt = append(cellAt, row.Len())
			if c := s.emu.CellAt(x, y); c != nil && c.Width > 0 {
				row.WriteString(c.Content)
			}
		}
		cellAt = append(cellAt, row.Len())
		i := strings.Index(row.String(), text)
		if i < 0 {
			continue
		}
		for x := 0; x < termW; x++ {
			if cellAt[x] <= i && i < cellAt[x+1] {
				return x, y
			}
		}
		return termW - 1, y
	}
	return -1, -1
}

func (s *session) send(b string) {
	s.t.Helper()
	if _, err := s.pt.WriteString(b); err != nil {
		s.t.Fatalf("write: %v", err)
	}
}

// click sends an SGR left press+release at 0-based x,y.
func (s *session) click(x, y int) {
	s.send(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+1, y+1))
}

// rightClick sends an SGR right press+release.
func (s *session) rightClick(x, y int) {
	s.send(fmt.Sprintf("\x1b[<2;%d;%dM\x1b[<2;%d;%dm", x+1, y+1, x+1, y+1))
}

func countPanes(screen string) int {
	// Pane titles look like " 1 zsh " on the top border of each pane.
	n := 0
	for _, line := range strings.Split(screen, "\n") {
		for m := 1; m <= 9; m++ {
			if strings.Contains(line, fmt.Sprintf(" %d ", m)) {
				n++
			}
		}
	}
	return n
}

func TestBootShowsSidebarAndPane(t *testing.T) {
	s := start(t)
	s.waitFor("sidebar footer", 10*time.Second, func(tx string) bool {
		return strings.Contains(tx, "[menu]") && strings.Contains(tx, "[new]") && strings.Contains(tx, "workspaces")
	})
	if !strings.Contains(s.text(), " gtx ") {
		t.Fatalf("missing status line brand:\n%s", s.text())
	}
}

func TestPrefixSplitVertical(t *testing.T) {
	s := start(t)
	s.waitFor("boot", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "[new]") })
	s.send("\x02v") // C-b v
	s.waitFor("two panes", 5*time.Second, func(tx string) bool {
		return countPanes(tx) >= 2
	})
}

func TestMouseFullFlow(t *testing.T) {
	s := start(t)
	s.waitFor("boot", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "[new]") })

	// 1. [new] creates a workspace.
	nx, ny := s.find("[new]")
	if nx < 0 {
		t.Fatal("[new] not found")
	}
	s.click(nx, ny)
	s.waitFor("workspace 2", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "1/2") || strings.Contains(tx, "workspace 2")
	})

	// 2. Click the first workspace row in the sidebar to go back.
	s.click(3, 1) // sidebar row for workspace 1
	s.waitFor("back on workspace 1", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "1/2") // status shows "workspace 1/2"
	})

	// 3. Right-click inside the pane → menu → Split vertical.
	px, _ := s.find("[new]")
	_ = px
	paneX, paneY := termW/2+14, termH/2
	s.rightClick(paneX, paneY)
	s.waitFor("menu", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "Split vertical")
	})
	mx, my := s.find("Split vertical")
	s.click(mx, my)
	s.waitFor("two panes", 5*time.Second, func(tx string) bool {
		return countPanes(tx) >= 2
	})

	// 4. [menu] → Quit → confirm exits 0.
	menX, menY := s.find("[menu]")
	s.click(menX, menY)
	s.waitFor("app menu", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "Quit")
	})
	qx, qy := s.find("Quit")
	s.click(qx, qy)
	s.waitFor("confirm dialog", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "quit") && strings.Contains(tx, "cancel")
	})
	cx, cy := s.find("quit")
	s.click(cx, cy)

	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("gtx exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("gtx did not exit")
	}
}

func TestSplitMouseReportProducesNoText(t *testing.T) {
	s := start(t)
	s.waitFor("boot", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "[new]") })
	// A mouse press sequence split across two writes must not leak
	// fragments like "35;61;8M" into the pane as text.
	s.send("\x1b[<35;60;8")
	time.Sleep(80 * time.Millisecond)
	s.send("M")
	time.Sleep(300 * time.Millisecond)
	s.pump(time.Now().Add(200 * time.Millisecond))
	if strings.Contains(s.text(), "35;60;8") || strings.Contains(s.text(), ";8M") {
		t.Fatalf("mouse fragment leaked as text:\n%s", s.text())
	}
}

func TestDividerDrag(t *testing.T) {
	s := start(t)
	s.waitFor("boot", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "[new]") })
	s.send("\x02v")
	s.waitFor("two panes", 5*time.Second, func(tx string) bool { return countPanes(tx) >= 2 })

	// Find the divider column: scan row 5 for a '│' run.
	divX := -1
	for x := 0; x < termW; x++ {
		if c := s.emu.CellAt(x, 5); c != nil && c.Content == "│" {
			divX = x
			break
		}
	}
	if divX < 0 {
		t.Fatalf("no divider found:\n%s", s.text())
	}
	// Drag the divider 10 columns right: press on divider, move, release.
	s.send(fmt.Sprintf("\x1b[<0;%d;6M", divX+1))
	s.send(fmt.Sprintf("\x1b[<32;%d;6M", divX+11))
	s.send(fmt.Sprintf("\x1b[<32;%d;6m", divX+11))
	s.waitFor("divider moved", 5*time.Second, func(string) bool {
		for x := 0; x < termW; x++ {
			if c := s.emu.CellAt(x, 5); c != nil && c.Content == "│" && x > divX+5 {
				return true
			}
		}
		return false
	})
}

func TestWheelScrollback(t *testing.T) {
	s := start(t)
	s.waitFor("boot", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "[new]") })
	// Wait for the shell prompt inside the pane before typing; under -race
	// the pane shell (zshrc/direnv) can take several seconds to be ready.
	s.waitFor("prompt", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.send("seq 1 200\n")
	// Wait until the finished output (a bare "200" row) appears — the typed
	// command itself contains "200", so match a row that is only "200".
	s.waitFor("seq output", 10*time.Second, func(tx string) bool {
		for _, l := range strings.Split(tx, "\n") {
			if strings.Contains(l, "│200") {
				return true
			}
		}
		return false
	})
	// Wheel up inside the pane region.
	px, py := termW/2+14, termH/2
	s.send(fmt.Sprintf("\x1b[<64;%d;%dM", px+1, py+1))
	s.send(fmt.Sprintf("\x1b[<64;%d;%dM", px+1, py+1))
	s.send(fmt.Sprintf("\x1b[<64;%d;%dM", px+1, py+1))
	s.waitFor("scroll indicator", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "↑")
	})
}

func TestDragSelectCopy(t *testing.T) {
	s := start(t)
	s.waitFor("boot", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "[new]") })
	s.waitFor("prompt", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.send("echo SELECT_ME\n")
	s.waitFor("echo output", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "SELECT_ME") })
	// Drag across the word: press inside the pane, move, release.
	x, y := s.find("SELECT_ME")
	if x < 0 {
		t.Fatalf("SELECT_ME not on screen:\n%s", s.text())
	}
	s.send(fmt.Sprintf("\x1b[<0;%d;%dM", x+1, y+1))
	s.send(fmt.Sprintf("\x1b[<32;%d;%dM", x+8, y+1))
	s.send(fmt.Sprintf("\x1b[<0;%d;%dm", x+8, y+1))
	// Releasing a moved selection must not crash; the copied flash appears.
	s.waitFor("copied flash", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "copied")
	})
}
