// ptyrun: run a TUI in a fixed-size PTY, drive it with a step script, and read its screen.
// usage: ptyrun -cols 160 -rows 45 -steps steps.txt -- cmd args...
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

func main() {
	cols := flag.Int("cols", 160, "")
	rows := flag.Int("rows", 45, "")
	steps := flag.String("steps", "", "")
	flag.Parse()
	args := flag.Args()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(*cols), Rows: uint16(*rows)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		os.Exit(1)
	}
	emu := vt.NewEmulator(*cols, *rows)
	var mu sync.Mutex
	go func() { _, _ = io.Copy(ptmx, emu) }() // emulator replies (DA etc.) back to the app
	// Drain the PTY as fast as possible (like a fast real terminal) and feed the emulator
	// from an unbounded queue, so the emulator's speed never throttles the app under test.
	var qmu sync.Mutex
	var queue [][]byte
	var drained int64
	go func() {
		buf := make([]byte, 1<<16)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				c := make([]byte, n)
				copy(c, buf[:n])
				qmu.Lock()
				queue = append(queue, c)
				qmu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		for {
			qmu.Lock()
			q := queue
			queue = nil
			qmu.Unlock()
			if len(q) == 0 {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			mu.Lock()
			for _, c := range q {
				_, _ = emu.Write(c)
				drained += int64(len(c))
			}
			mu.Unlock()
		}
	}()
	settle := func() { // wait until the emulator caught up with everything read so far
		deadline := time.Now().Add(120 * time.Second)
		for time.Now().Before(deadline) {
			qmu.Lock()
			empty := len(queue) == 0
			qmu.Unlock()
			if empty {
				time.Sleep(50 * time.Millisecond)
				qmu.Lock()
				empty = len(queue) == 0
				qmu.Unlock()
				if empty {
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	screen := func() string {
		settle()
		mu.Lock()
		defer mu.Unlock()
		var b strings.Builder
		for y := 0; y < *rows; y++ {
			var line strings.Builder
			for x := 0; x < *cols; x++ {
				c := emu.CellAt(x, y)
				if c == nil || c.Content == "" {
					if c != nil && c.Width == 0 {
						continue
					}
					line.WriteByte(' ')
					continue
				}
				line.WriteString(c.Content)
			}
			b.WriteString(strings.TrimRight(line.String(), " "))
			b.WriteByte('\n')
		}
		return b.String()
	}
	f, err := os.Open(*steps)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		op, rest, _ := strings.Cut(line, " ")
		switch op {
		case "sleep":
			d, _ := time.ParseDuration(rest)
			time.Sleep(d)
		case "send":
			s, err := strconv.Unquote(rest)
			if err != nil {
				fmt.Fprintln(os.Stderr, "bad send:", rest)
				os.Exit(1)
			}
			_, _ = ptmx.Write([]byte(s))
		case "click": // click X Y (1-based cells), left button
			var x, y int
			fmt.Sscanf(rest, "%d %d", &x, &y)
			_, _ = fmt.Fprintf(ptmx, "\x1b[<0;%d;%dM", x, y)
			time.Sleep(30 * time.Millisecond)
			_, _ = fmt.Fprintf(ptmx, "\x1b[<0;%d;%dm", x, y)
		case "rclick":
			var x, y int
			fmt.Sscanf(rest, "%d %d", &x, &y)
			_, _ = fmt.Fprintf(ptmx, "\x1b[<2;%d;%dM", x, y)
			time.Sleep(30 * time.Millisecond)
			_, _ = fmt.Fprintf(ptmx, "\x1b[<2;%d;%dm", x, y)
		case "screen":
			_ = os.WriteFile(rest, []byte(screen()), 0o644)
		case "waitfile": // waitfile PATH TIMEOUT
			p, ts, _ := strings.Cut(rest, " ")
			d, _ := time.ParseDuration(ts)
			deadline := time.Now().Add(d)
			for {
				if st, err := os.Stat(p); err == nil && st.Size() > 0 {
					break
				}
				if time.Now().After(deadline) {
					fmt.Println("TIMEOUT waiting", p)
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		case "stats": // rss and cpu time of the root process
			out, _ := exec.Command("ps", "-o", "rss=,time=", "-p", strconv.Itoa(cmd.Process.Pid)).Output()
			mu.Lock()
			d := drained
			mu.Unlock()
			fmt.Printf("stats %s pid=%d rss_kb,cpu=%s outer_bytes_so_far=%d\n", rest, cmd.Process.Pid, strings.TrimSpace(string(out)), d)
		case "kill":
			_ = cmd.Process.Kill()
		default:
			fmt.Fprintln(os.Stderr, "unknown op:", op)
			os.Exit(1)
		}
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}
