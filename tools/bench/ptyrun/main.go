// ptyrun: run a TUI in a fixed-size PTY, drive it with a step script, and read its screen.
// usage: ptyrun -cols 160 -rows 45 -steps steps.txt -- cmd args...
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

func main() {
	cols := flag.Int("cols", 160, "")
	rows := flag.Int("rows", 45, "")
	steps := flag.String("steps", "", "")
	xpixel := flag.Int("xpixel", 0, "PTY window width in pixels")
	ypixel := flag.Int("ypixel", 0, "PTY window height in pixels")
	cellWidth := flag.Int("cell-width", 0, "terminal character width in pixels for size queries")
	cellHeight := flag.Int("cell-height", 0, "terminal character height in pixels for size queries")
	pixelMouse := flag.Bool("pixel-mouse", true, "outer terminal supports SGR pixel mouse")
	rawOutput := flag.String("raw-output", "", "save raw terminal output")
	flag.Parse()
	args := flag.Args()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(*cols), Rows: uint16(*rows), X: uint16(*xpixel), Y: uint16(*ypixel)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		os.Exit(1)
	}
	emu := vt.NewEmulator(*cols, *rows)
	if *cellWidth == 0 && *xpixel > 0 {
		*cellWidth = *xpixel / *cols
	}
	if *cellHeight == 0 && *ypixel > 0 {
		*cellHeight = *ypixel / *rows
	}
	emu.RegisterCsiHandler('t', func(params ansi.Params) bool {
		op, _, _ := params.Param(0, 0)
		if *cellWidth <= 0 || *cellHeight <= 0 {
			return false
		}
		switch op {
		case 16:
			fmt.Fprintf(ptmx, "\x1b[6;%d;%dt", *cellHeight, *cellWidth)
		case 14:
			fmt.Fprintf(ptmx, "\x1b[4;%d;%dt", *rows**cellHeight, *cols**cellWidth)
		default:
			return false
		}
		return true
	})
	if !*pixelMouse {
		emu.RegisterCsiHandler(ansi.Command('?', '$', 'p'), func(params ansi.Params) bool {
			mode, _, _ := params.Param(0, 0)
			if mode != 1016 {
				return false
			}
			fmt.Fprint(ptmx, "\x1b[?1016;0$y")
			return true
		})
	}
	var rawFile *os.File
	if *rawOutput != "" {
		rawFile, err = os.Create(*rawOutput)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer rawFile.Close()
	}
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
				if rawFile != nil {
					_, _ = rawFile.Write(buf[:n])
				}
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
	var sentAt time.Time
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
			sentAt = time.Now()
		case "mouse", "pixel": // kind, then one-based coordinates; mouse uses cells, pixel preserves subcell precision
			var kind string
			var x, y int
			if _, err := fmt.Sscanf(rest, "%s %d %d", &kind, &x, &y); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			m := uv.Mouse{X: x - 1, Y: y - 1, Button: uv.MouseLeft}
			mu.Lock()
			if op == "mouse" && emu.ModeSet(ansi.ModeMouseExtSgrPixel) {
				if *cellWidth <= 0 || *cellHeight <= 0 {
					mu.Unlock()
					fmt.Fprintln(os.Stderr, "pixel mouse needs cell size")
					os.Exit(1)
				}
				m.X = m.X**cellWidth + *cellWidth/2
				m.Y = m.Y**cellHeight + *cellHeight/2
			}
			switch kind {
			case "down":
				emu.SendMouse(uv.MouseClickEvent(m))
			case "up":
				emu.SendMouse(uv.MouseReleaseEvent(m))
			case "motion":
				emu.SendMouse(uv.MouseMotionEvent(m))
			case "wheel-up":
				m.Button = uv.MouseWheelUp
				emu.SendMouse(uv.MouseWheelEvent(m))
			case "wheel-down":
				m.Button = uv.MouseWheelDown
				emu.SendMouse(uv.MouseWheelEvent(m))
			default:
				mu.Unlock()
				fmt.Fprintln(os.Stderr, "unknown mouse kind", kind)
				os.Exit(1)
			}
			mu.Unlock()
		case "waittext": // waittext TIMEOUT TEXT: report time since the last send until TEXT is on screen
			ts, text, _ := strings.Cut(rest, " ")
			d, _ := time.ParseDuration(ts)
			deadline := time.Now().Add(d)
			for !strings.Contains(screen(), text) {
				if time.Now().After(deadline) {
					fmt.Println("TIMEOUT waiting text", text)
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			fmt.Printf("onscreen %.2f\n", time.Since(sentAt).Seconds())
		case "click": // click X Y (1-based cells), left button
			var x, y int
			fmt.Sscanf(rest, "%d %d", &x, &y)
			_, _ = fmt.Fprintf(ptmx, "\x1b[<0;%d;%dM", x, y)
			time.Sleep(30 * time.Millisecond)
			_, _ = fmt.Fprintf(ptmx, "\x1b[<0;%d;%dm", x, y)
		case "clicktext":
			label, err := strconv.Unquote(rest)
			if err != nil || label == "" {
				fmt.Fprintln(os.Stderr, "bad clicktext:", rest)
				os.Exit(1)
			}
			x, y, found := 0, 0, 0
			for row, line := range strings.Split(screen(), "\n") {
				if col := strings.Index(line, label); col >= 0 {
					x, y = ansi.StringWidth(line[:col])+1, row+1
					found++
				}
			}
			if found != 1 {
				fmt.Fprintf(os.Stderr, "clicktext %q requires one visible match, got %d\n", label, found)
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
				os.Exit(1)
			}
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
		case "cells":
			settle()
			mu.Lock()
			grid := make([][]uv.Cell, *rows)
			for y := range grid {
				grid[y] = make([]uv.Cell, *cols)
				for x := range grid[y] {
					if cell := emu.CellAt(x, y); cell != nil {
						grid[y][x] = *cell
					}
				}
			}
			data, err := json.Marshal(struct {
				Cols, Rows     int
				Cells          [][]uv.Cell
				CapturedUnixMs int64
				LastSendUnixMs int64
			}{*cols, *rows, grid, time.Now().UnixMilli(), sentAt.UnixMilli()})
			mu.Unlock()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			if err = os.WriteFile(rest, data, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		case "resize":
			var c, r int
			if _, err := fmt.Sscanf(rest, "%d %d", &c, &r); err != nil || c < 1 || r < 1 {
				fmt.Fprintln(os.Stderr, "bad resize:", rest)
				os.Exit(1)
			}
			mu.Lock()
			*cols, *rows = c, r
			emu.Resize(c, r)
			mu.Unlock()
			if err := pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(c), Rows: uint16(r)}); err != nil {
				fmt.Fprintln(os.Stderr, "resize:", err)
				os.Exit(1)
			}
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
			if owner, err := strconv.Atoi(os.Getenv("PARITY_OBSERVE_PID")); err == nil && owner > 0 {
				processes, err := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				lines := strings.Split(string(processes), "\n")
				owned := map[int]bool{owner: true}
				for changed := true; changed; {
					changed = false
					for _, line := range lines {
						fields := strings.Fields(line)
						if len(fields) < 3 {
							continue
						}
						pid, _ := strconv.Atoi(fields[0])
						parent, _ := strconv.Atoi(fields[1])
						if owned[parent] && !owned[pid] {
							owned[pid], changed = true, true
						}
					}
				}
				for _, line := range lines {
					fields := strings.Fields(line)
					if len(fields) > 0 {
						pid, _ := strconv.Atoi(fields[0])
						if owned[pid] {
							fmt.Printf("owned-process %s %s\n", rest, strings.TrimSpace(line))
						}
					}
				}
			}
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
