// Package e2e boots the real ikesu binary on a PTY and drives it with raw
// input bytes, reading the screen through a vt.Emulator — the same things a
// user terminal does.
package e2e

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/teststate"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
var keepFailedE2EState = os.Getenv("IKESU_E2E_KEEP_FAILED") == "1"

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ikesu-e2e")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	binPath = filepath.Join(dir, "ikesu")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/ikesu")
	cmd.Dir = repoRoot()
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic("build ikesu: " + err.Error())
	}
	code := teststate.Run(m.Run)
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, "remove e2e build directory:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func repoRoot() string {
	wd, _ := os.Getwd()
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// session is a running ikesu under a PTY with its screen decoded to an
// emulator.
type session struct {
	t             *testing.T
	pt            *os.File
	cmd           *exec.Cmd
	emu           *vt.Emulator
	buf           *lockedBuf
	name          string // session name (ikesu --session)
	stateDir      string // IKESU_STATE_DIR
	observeOutput func([]byte)
}

type lockedBuf struct {
	ch chan []byte
}

func start(t *testing.T) *session { return startEnv(t, nil) }

func startLocal(t *testing.T) *session { return startMode(t, nil, true) }

func startEnv(t *testing.T, extra map[string]string) *session {
	return startMode(t, extra, false)
}

func startMode(t *testing.T, extra map[string]string, local bool) *session {
	return startVersionMode(t, extra, local, binPath, "")
}

func startVersionMode(t *testing.T, extra map[string]string, local bool, clientBinary, serverBinary string) *session {
	t.Helper()
	// Deterministic shell inside panes: a bare ZDOTDIR with a fixed PS1 so
	// prompt waits do not depend on the host's zshrc/direnv.
	zdot := t.TempDir()
	// Short root: unix socket paths must stay under ~104 bytes.
	stateDir, err := os.MkdirTemp("/tmp", "ikesu-e2e")
	if err != nil {
		t.Fatalf("state dir: %v", err)
	}
	t.Cleanup(func() {
		if t.Failed() && keepFailedE2EState {
			t.Logf("preserved failed E2E state: %s", stateDir)
			return
		}
		os.RemoveAll(stateDir)
	})
	if err := os.WriteFile(filepath.Join(zdot, ".zshrc"), []byte("PS1='❯ '\n"), 0o644); err != nil {
		t.Fatalf("zshrc: %v", err)
	}
	sess := strings.ReplaceAll(t.Name(), "/", "_")
	// Disable startup auto-resume so a restored session can never launch
	// a real agent binary inside a test.
	cfgDir := t.TempDir()
	cfgFile := filepath.Join(cfgDir, "config.toml")
	os.WriteFile(cfgFile, []byte("onboarding=false\n[session]\nresume_agents_on_restore = false\n"), 0o600)
	cmd := exec.Command(clientBinary, "--session", sess)
	if local {
		cmd = exec.Command(clientBinary, "--no-session")
	}
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"SHELL=/bin/zsh",
		"ZDOTDIR="+zdot,
		"IKESU_STATE_DIR="+stateDir,
		"IKESU_CONFIG="+cfgFile,
	)
	for k, v := range extra {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if serverBinary != "" {
		server := exec.Command(serverBinary, "--session", sess, "server")
		server.Env = cmd.Env
		server.Stderr = os.Stderr
		if err := server.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			(&session{t: t, stateDir: stateDir, name: sess}).stopServer()
			if err := server.Wait(); err != nil {
				t.Errorf("mixed server cleanup: %v", err)
			}
		})
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(stateDir, sess, "ikesu-client.sock")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				_ = server.Process.Kill()
				t.Fatal("mixed server did not bind client socket")
			}
			time.Sleep(time.Millisecond)
		}
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: termH, Cols: termW})
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}
	s := &session{t: t, pt: f, cmd: cmd, emu: vt.NewEmulator(termW, termH), buf: &lockedBuf{ch: make(chan []byte, 4096)}, name: sess, stateDir: stateDir}
	// Consume emulator replies without sending them to ikesu: this outer
	// terminal deliberately leaves every capability query unanswered.
	// The emulator's reply pipe must still be read or its screen parser blocks.
	go func() { _, _ = io.Copy(io.Discard, s.emu) }()
	t.Cleanup(func() { s.emu.Close() })
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
		if local {
			// Let standalone mode stop its pane processes before deleting ZDOTDIR.
			_, _ = f.WriteString("\x02q")
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			select {
			case err := <-exited:
				if err != nil {
					t.Errorf("standalone cleanup: %v", err)
				}
			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				<-exited
				t.Error("standalone did not exit during cleanup")
			}
		}
		_ = f.Close()
		if !local && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		// The server outlives its client; ask it to stop so the test
		// session's panes and socket do not linger.
		if !local {
			s.stopServer()
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
			if s.observeOutput != nil {
				s.observeOutput(d)
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

// cli runs the ikesu binary against this test session and returns stdout.
func (s *session) cli(args ...string) (string, error) {
	s.t.Helper()
	cmd := exec.Command(binPath, append([]string{"--session", s.name}, args...)...)
	cmd.Env = append(os.Environ(), "IKESU_STATE_DIR="+s.stateDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// click sends an SGR left press+release at 0-based x,y.
func (s *session) click(x, y int) {
	s.send(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+1, y+1))
}

// rightClick sends an SGR right press+release.
func (s *session) rightClick(x, y int) {
	s.send(fmt.Sprintf("\x1b[<2;%d;%dM\x1b[<2;%d;%dm", x+1, y+1, x+1, y+1))
}

var paneTitleRe = regexp.MustCompile(` %\d+ \S`)

func countPanes(screen string) int {
	// Pane titles look like " 1 zsh " on a pane border row — a digit
	// followed immediately by a word char. Skipping row 0 keeps the tab
	// strip (" tab N ") out, and the sidebar's " 1   workspace 1" does not
	// match (spaces after the digit).
	n := 0
	for i, line := range strings.Split(screen, "\n") {
		if i == 0 {
			continue
		}
		n += len(paneTitleRe.FindAllString(line, -1))
	}
	return n
}

func TestBootShowsSidebarAndPane(t *testing.T) {
	s := start(t)
	s.waitFor("sidebar footer and status line", 10*time.Second, func(tx string) bool {
		return strings.Contains(tx, "[menu]") &&
			strings.Contains(tx, "[new]") &&
			strings.Contains(tx, " spaces") &&
			countPanes(tx) == 1 &&
			strings.Contains(tx, " NORMAL ") &&
			!strings.Contains(tx, "[full]")
	})
	if !strings.Contains(s.text(), " NORMAL ") || strings.Contains(s.text(), "[full]") {
		t.Fatalf("server must show the 0.2.24 width button and plain header:\n%s", s.text())
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
	// Git discovery can replace the initial cwd label before we see the frame.
	secondWorkspace := regexp.MustCompile(`(?m)^[▌· ]*2 \S`)
	s.waitFor("workspace 2", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, " %2 terminal ") && secondWorkspace.MatchString(tx)
	})

	// 2. Click the first workspace row in the sidebar to go back.
	s.click(3, 5) // first workspace card below the section header
	s.waitFor("back on workspace 1", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, " %1 terminal ")
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

	// 4. [menu] → Stop server → confirm exits 0.
	menX, menY := s.find("[menu]")
	s.click(menX, menY)
	s.waitFor("app menu", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "stop server")
	})
	qx, qy := s.find("stop server")
	s.click(qx, qy)
	s.waitFor("confirm dialog", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "Stop server?") && strings.Contains(tx, "cancel")
	})
	cx, cy := s.find("stop")
	s.click(cx, cy)

	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ikesu exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ikesu did not exit")
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
	s := startLocal(t)
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
	// Releasing a moved selection must not crash; the copied flash appears
	// on the sidebar's bottom row (SPEC G1 "Copied N lines").
	s.waitFor("copied flash", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "Copied")
	})
}

// SPEC G6: prefix+[ enters copy mode; / searches, Space anchors,
// Enter yanks and exits with a "Copied N lines" sidebar flash.
func TestCopyModeSelectYank(t *testing.T) {
	s := startLocal(t)
	s.waitFor("boot", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "[new]") })
	s.waitFor("prompt", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.send("echo COPYMARK\n")
	s.waitFor("echo output", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "COPYMARK") })

	s.send("\x02[") // prefix+[
	s.waitFor("copy mode", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "PHONE COPY")
	})

	// Search for the marker, anchor, extend right, yank with Enter.
	s.send("/COPYMARK\r")
	s.send(" llll")
	s.send("\r")
	s.waitFor("yank flash", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "Copied 1 line")
	})
	s.waitFor("back to copy label", 5*time.Second, func(tx string) bool {
		return !strings.Contains(tx, "PHONE COPY") && strings.Contains(tx, " COPY ")
	})
}

// The observed default right-bracket prefix is ignored; configured entry is tested separately.
func TestCopyModeRightBracket(t *testing.T) {
	s := start(t)
	s.waitFor("boot", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "[new]") })
	s.send("\x02]")
	s.send("printf 'RIGHT_%s\\n' BRACKET\r")
	s.waitFor("ignored prefix leaves shell input available", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "RIGHT_BRACKET") && !strings.Contains(tx, "PHONE COPY")
	})
}

// SPEC G1/G6: the action bar COPY button cycles normal → copy →
// fullscreen copy → normal.
func TestActionBarCopyCycle(t *testing.T) {
	s := startLocal(t)
	s.waitFor("COPY button", 10*time.Second, func(tx string) bool { return strings.Contains(tx, " COPY ") })
	x, y := s.find(" COPY ")
	if x < 0 {
		t.Fatalf("COPY button not found:\n%s", s.text())
	}
	s.click(x, y)
	s.waitFor("phone copy", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "PHONE COPY")
	})
	x, y = s.find("PHONE COPY")
	s.click(x, y)
	s.waitFor("exit copy label", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "EXIT COPY")
	})
	x, y = s.find("EXIT COPY")
	s.click(x, y)
	s.waitFor("back to normal", 5*time.Second, func(tx string) bool {
		return !strings.Contains(tx, "PHONE COPY") && !strings.Contains(tx, "EXIT COPY") && strings.Contains(tx, " COPY ")
	})
}

// Default split keys follow the observed distribution bindings.
func TestTmuxSplitKeys(t *testing.T) {
	s := start(t)
	s.waitFor("boot", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "[new]") })
	s.send("\x02v") // observed vertical binding
	s.waitFor("vertical split", 5*time.Second, func(tx string) bool {
		return countPanes(tx) >= 2
	})
	s.send("\x02-") // observed horizontal binding
	s.waitFor("horizontal split", 5*time.Second, func(tx string) bool {
		return countPanes(tx) >= 3
	})
}

// G3/G5: ikesu server restart saves the session, respawns the server, and
// the attached client reconnects to restored panes with stable ids.
func TestServerRestartKeepsPanes(t *testing.T) {
	s := start(t)
	s.waitFor("boot", 10*time.Second, func(tx string) bool {
		return strings.Contains(tx, "[new]")
	})

	// Two panes in the initial workspace.
	s.send("\x02v") // C-b v
	s.waitFor("two panes", 5*time.Second, func(tx string) bool {
		return countPanes(tx) >= 2
	})

	// Restart through the CLI: it returns after the successor answers ping.
	out, err := s.cli("server", "restart")
	if err != nil {
		t.Fatalf("server restart: %v\n%s", err, out)
	}

	// The attached client must still show both panes after reconnecting
	// to the restarted server.
	s.waitFor("restored panes", 15*time.Second, func(tx string) bool {
		return countPanes(tx) >= 2
	})

	// Pane numbers survived the restart.
	listOut, err := s.cli("pane", "list")
	if err != nil {
		t.Fatalf("pane list: %v\n%s", err, listOut)
	}
	if !strings.Contains(listOut, `"pane_id":"p1"`) || !strings.Contains(listOut, `"pane_id":"p2"`) {
		t.Fatalf("pane ids not stable across restart:\n%s", listOut)
	}
}

// SPEC G6: a raw LF byte reaches the pane as LF, not normalized to CR.
// od -tx1 gives byte-level proof: typing "A" then a bare LF yields
// "41 0a" with no 0d anywhere on the wire.
func TestRawLFToPane(t *testing.T) {
	s := start(t)
	s.waitFor("prompt", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	prepareByteReader(s, 2)
	s.send("A\n")
	s.waitFor("od bytes", 10*time.Second, func(tx string) bool {
		return strings.Contains(strings.Join(strings.Fields(tx), " "), "41 0a")
	})
	if strings.Contains(s.text(), "0d") {
		t.Fatalf("LF was translated to CR:\n%s", s.text())
	}
}

// Herdr 0.2.56 preserves the FixTerm form for an ordinary pane, drops
// releases there, and converts the same press to CSI u for a kitty pane.
func TestShiftEnterToPane(t *testing.T) {
	s := start(t)
	s.waitFor("prompt", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	prepareByteReader(s, 10)
	s.send("\x1b[13;2:3u")
	s.send("\x1b[27;2;13~")
	s.waitFor("FixTerm press only", 10*time.Second, func(tx string) bool {
		return strings.Contains(strings.Join(strings.Fields(tx), " "), "1b 5b 32 37 3b 32 3b 31 33 7e")
	})
}

func TestKittyExplicitShiftEnter(t *testing.T) {
	s := start(t)
	s.waitFor("prompt", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.send("printf '\\033[>1u'\r")
	s.waitFor("push done", 5*time.Second, func(tx string) bool { return strings.Count(tx, "❯") >= 2 })
	prepareByteReader(s, 7)
	s.send("\x1b[27;2;13~")
	s.waitFor("kitty encoded", 10*time.Second, func(tx string) bool {
		return strings.Contains(strings.Join(strings.Fields(tx), " "), "1b 5b 31 33 3b 32 75")
	})
}

// SPEC G6: a FixTerm keyboard query (CSI ? u) from the pane is answered
// with CSI ? 0 u — the response bytes land in the pane's input stream.
func TestFixTermQueryResponse(t *testing.T) {
	s := start(t)
	s.waitFor("prompt", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	// The response is queued on the tty input buffer and read by od.
	s.send("stty -icanon; printf '\\033[?u'; od -An -tx1 -N5\r")
	s.waitFor("query response", 10*time.Second, func(tx string) bool {
		return strings.Contains(strings.Join(strings.Fields(tx), " "), "1b 5b 3f 30 75")
	})
}

// SPEC G6: NO_COLOR is stripped from pane environments while COLORTERM
// and TERM survive.
func TestPaneEnvNoColor(t *testing.T) {
	s := startEnv(t, map[string]string{"NO_COLOR": "1"})
	s.waitFor("prompt", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.send("env | grep -E 'NO_COLOR|COLORTERM|^TERM='\r")
	s.waitFor("env output", 10*time.Second, func(tx string) bool {
		return strings.Contains(tx, "COLORTERM=truecolor")
	})
	tx := s.text()
	if strings.Contains(tx, "NO_COLOR=") {
		t.Fatalf("NO_COLOR leaked into pane env:\n%s", tx)
	}
	if !strings.Contains(tx, "TERM=xterm-256color") {
		t.Fatalf("TERM missing:\n%s", tx)
	}
}

// SPEC G6: right-click on an inactive tab selects it and opens its menu.

// SPEC G6: prefix+? opens the keybind help overlay; wheel scrolls it; a
// click outside closes it.
func TestKeybindHelp(t *testing.T) {
	s := start(t)
	s.waitFor("boot", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "[new]") })
	s.send("\x02?")
	s.waitFor("help open", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "keybinds") && strings.Contains(tx, "available commands and configured shortcuts") &&
			strings.Contains(tx, "cycle pane")
	})
	// Wheel inside the overlay scrolls without closing.
	s.send(fmt.Sprintf("\x1b[<65;%d;%dM", termW/2+1, termH/2+1))
	s.pump(time.Now().Add(300 * time.Millisecond))
	if !strings.Contains(s.text(), "keybinds") {
		t.Fatalf("wheel closed the help overlay:\n%s", s.text())
	}
	// Click outside the box closes it.
	s.click(2, termH-1)
	s.waitFor("help closed", 5*time.Second, func(tx string) bool {
		return !strings.Contains(tx, "keybinds")
	})
}

func TestBootWithoutTerminalQueryRepliesAllowsInputAndLateReplies(t *testing.T) {
	s := start(t)
	var raw strings.Builder
	s.observeOutput = func(b []byte) { raw.Write(b) }
	s.waitFor("boot without query replies", 10*time.Second, func(tx string) bool {
		return strings.Contains(tx, "[new]") && countPanes(tx) == 1
	})
	for _, query := range []string{"\x1b[?1016$p", "\x1b[16t", "\x1b[14t"} {
		if !strings.Contains(raw.String(), query) {
			t.Fatalf("query missing: %q", query)
		}
	}
	s.send("printf 'NO_REPLY_%s\\n' READY\r")
	s.waitFor("input before terminal replies", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "NO_REPLY_READY") })
	s.send("\x1b[?1016;2$y\x1b[6;32;16t\x1b[4;1280;1920t")
	s.waitFor("late pixel capability applied", 5*time.Second, func(string) bool { return strings.Contains(raw.String(), "\x1b[?1016h") })
	s.send("printf 'LATE_REPLY_%s\\n' READY\r")
	s.waitFor("input after terminal replies", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "LATE_REPLY_READY") })
}

// Synchronize with the command rather than letting zsh consume typeahead
// while it is still preparing od. Escape the marker so command echo cannot
// satisfy the readiness check; inspect bytes with tty translations disabled.
func prepareByteReader(s *session, n int) {
	s.send(fmt.Sprintf("saved=$(stty -g); stty -icanon -echo -icrnl -inlcr; printf '\\122\\105\\101\\104\\131\\137\\102\\131\\124\\105\\123\\n'; od -An -tx1 -N%d; stty \"$saved\"\r", n))
	s.waitFor("byte reader ready", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "READY_BYTES") })
}
