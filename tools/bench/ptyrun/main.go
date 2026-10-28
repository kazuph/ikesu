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
	fileImages := flag.String("file-images", "", "record file-image payload hashes while the viewer owns the files")
	relayLog := flag.String("claude-relay-log", "", "observe unmodified Claude Code without forwarding conversation input")
	relayExit := flag.String("claude-relay-exit", "", "owned controller file requesting ordinary Ctrl-C exit")
	includeBodies := flag.Bool("body-grids", false, "record real pane text for copied Claude conversations")
	processPath := flag.String("process-file", "", "save the measured product PID")
	timelinePath := flag.String("timeline", "", "monotonic synchronized-frame and workload-marker records")
	cellClock := flag.Bool("cell-clock", false, "record the PTY read time when each cell content changes")
	statusDelay := flag.String("status-delay", "", "override terminal status replies with a duration or drop")
	holdColor := flag.Bool("hold-color-replies", false, "leave background-color queries for explicit steps to answer")
	flag.Parse()
	args := flag.Args()
	if *relayLog != "" {
		runClaudeRelay(args, *relayLog, *relayExit)
		return
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(*cols), Rows: uint16(*rows), X: uint16(*xpixel), Y: uint16(*ypixel)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		os.Exit(1)
	}
	if *processPath != "" {
		data, _ := json.Marshal(map[string]any{"pid": cmd.Process.Pid, "clock_ns": monotonicNS(), "args": args})
		if err := os.WriteFile(*processPath, data, 0600); err != nil {
			panic(err)
		}
	}
	emu := vt.NewEmulator(*cols, *rows)

	if *holdColor {
		emu.RegisterOscHandler(11, func(data []byte) bool {
			fmt.Printf("held-color-report %q\n", data)
			return true
		})
	}
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
	var chunkEnd int64
	if *fileImages != "" {
		log, err := os.Create(*fileImages)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer log.Close()
		emu.RegisterApcHandler(func(data []byte) bool {
			captureFileImage(log, data, chunkEnd)
			return false
		})
	}
	observer := newTimeline(*timelinePath, emu, *cols, *rows)
	if observer != nil {
		observer.reply = ptmx
		observer.receiver.reply = ptmx
		observer.receiver.cellW, observer.receiver.cellH = *cellWidth, *cellHeight
		observer.includeBodies = *includeBodies
	}
	defer observer.close()
	if *statusDelay != "" {
		delay, err := time.ParseDuration(*statusDelay)
		if *statusDelay != "drop" && (err != nil || delay < 0) {
			fmt.Fprintln(os.Stderr, "invalid status reply delay:", *statusDelay)
			os.Exit(1)
		}
		emu.RegisterCsiHandler('n', func(params ansi.Params) bool {
			op, _, _ := params.Param(0, 0)
			if op != 5 {
				return false
			}
			if observer != nil {
				if len(observer.receiver.pending) != 0 {
					panic("status query before image payload completed")
				}
				observer.record(map[string]any{"type": "terminal.status_pending", "clock_ns": monotonicNS(), "policy": *statusDelay})
			}
			fmt.Printf("status-query at_unix_us=%d\n", time.Now().UnixMicro())
			if *statusDelay != "drop" {
				go func() {
					time.Sleep(delay)
					_, err := io.WriteString(ptmx, "\x1b[0n")
					fmt.Printf("status-reply at_unix_us=%d error=%v\n", time.Now().UnixMicro(), err)
				}()
			}
			return true
		})
	}
	var mu sync.Mutex
	go func() { _, _ = io.Copy(ptmx, emu) }() // emulator replies (DA etc.) back to the app
	// Drain the PTY as fast as possible (like a fast real terminal) and feed the emulator
	// from an unbounded queue, so the emulator's speed never throttles the app under test.
	var qmu sync.Mutex
	type outputChunk struct {
		data []byte
		at   int64
		mono int64
	}
	var queue []outputChunk
	queueWake := make(chan struct{}, 1)
	var cellChanged [][]int64
	var cellContent [][]string
	var drained int64
	var completeAt int64
	go func() {
		buf := make([]byte, 1<<16)
		for {
			n, err := ptmx.Read(buf)
			readMono, readWall := monotonicNS(), time.Now().UnixMilli()
			if n > 0 {
				c := make([]byte, n)
				copy(c, buf[:n])
				qmu.Lock()
				queue = append(queue, outputChunk{data: c, at: readWall, mono: readMono})
				select {
				case queueWake <- struct{}{}:
				default:
				}
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
				<-queueWake
				continue
			}
			mu.Lock()
			for _, c := range q {
				if rawFile != nil {
					_, _ = rawFile.Write(c.data)
				}
				chunkEnd = drained + int64(len(c.data))
				if observer != nil {
					observer.feed(c.data, c.mono)
				} else {
					_, _ = emu.Write(c.data)
				}
				drained += int64(len(c.data))
				if !emu.ModeSet(ansi.ModeSynchronizedOutput) {
					completeAt = c.at
				}
				if *cellClock {
					if len(cellChanged) != *rows || len(cellChanged[0]) != *cols {
						cellChanged, cellContent = make([][]int64, *rows), make([][]string, *rows)
						for y := range cellChanged {
							cellChanged[y], cellContent[y] = make([]int64, *cols), make([]string, *cols)
						}
					}
					for y := range cellChanged {
						for x := range cellChanged[y] {
							content := ""
							if cell := emu.CellAt(x, y); cell != nil {
								content = cell.Content
							}
							if content != cellContent[y][x] {
								cellContent[y][x], cellChanged[y][x] = content, c.at
							}
						}
					}
				}
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
	locations := map[string][2]int{}
	mark := func(kind, label string) {
		mu.Lock()
		count := drained
		mu.Unlock()
		fmt.Printf("event %s label=%q at_unix_ms=%d outer_bytes=%d\n", kind, label, time.Now().UnixMilli(), count)
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		op, rest, _ := strings.Cut(line, " ")
		switch op {
		case "mark":
			mark("mark", rest)
		case "sleep":
			d, _ := time.ParseDuration(rest)
			time.Sleep(d)
		case "send", "sendtimed":
			s, err := strconv.Unquote(rest)
			if err != nil {
				fmt.Fprintln(os.Stderr, "bad send:", rest)
				os.Exit(1)
			}
			if op == "sendtimed" {
				at := monotonicNS()
				if _, err := ptmx.Write([]byte(s)); err != nil {
					panic(err)
				}
				observer.record(map[string]any{"type": "capacity.input", "clock_ns": at, "data": s})
			} else {
				_, _ = ptmx.Write([]byte(s))
			}
			sentAt = time.Now()
		case "wheelburst": // COUNT HZ X Y: timed scroll input, independent of render completion
			var count, rate, x, y int
			if n, _ := fmt.Sscanf(rest, "%d %d %d %d", &count, &rate, &x, &y); n != 4 || count <= 0 || rate <= 0 || time.Second/time.Duration(rate) <= 0 {
				fmt.Fprintln(os.Stderr, "bad wheelburst:", rest)
				os.Exit(1)
			}
			mu.Lock()
			pixel := emu.ModeSet(ansi.ModeMouseExtSgrPixel)
			if pixel {
				if *cellWidth <= 0 || *cellHeight <= 0 {
					mu.Unlock()
					fmt.Fprintln(os.Stderr, "pixel wheel needs cell size")
					os.Exit(1)
				}
				x, y = (x-1)**cellWidth+*cellWidth/2+1, (y-1)**cellHeight+*cellHeight/2+1
			}
			mu.Unlock()
			start := time.Now()
			times := make([]int64, 0, count)
			for i := 0; i < count; i++ {
				time.Sleep(time.Until(start.Add(time.Duration(i) * time.Second / time.Duration(rate))))
				times = append(times, time.Now().UnixMicro())
				_, _ = fmt.Fprintf(ptmx, "\x1b[<65;%d;%dM", x, y)
			}
			time.Sleep(time.Until(start.Add(time.Duration(count) * time.Second / time.Duration(rate))))
			mu.Lock()
			sameMode := pixel == emu.ModeSet(ansi.ModeMouseExtSgrPixel)
			mu.Unlock()
			data, _ := json.Marshal(times)
			fmt.Printf("wheelburst count=%d rate=%d times_unix_us=%s\n", count, rate, data)
			if !sameMode {
				fmt.Fprintln(os.Stderr, "mouse coordinate mode changed during timed scroll")
				os.Exit(1)
			}
		case "notifyfile":
			if err := os.WriteFile(rest, []byte("ready\n"), 0600); err != nil {
				panic(err)
			}
		case "waitmarkers":
			var n int
			var duration string
			if _, err := fmt.Sscanf(rest, "%d %s", &n, &duration); err != nil {
				panic(err)
			}
			timeout, err := time.ParseDuration(duration)
			if err != nil {
				panic(err)
			}
			deadline := time.Now().Add(timeout)
			for {
				mu.Lock()
				count := len(observer.latest.Panes)
				mu.Unlock()
				if count == n {
					break
				}
				if time.Now().After(deadline) {
					fmt.Fprintln(os.Stderr, screen())
					panic("workload markers did not appear")
				}
				time.Sleep(5 * time.Millisecond)
			}
		case "panecommands":
			if observer == nil {
				panic("panecommands needs -timeline")
			}
			settle()
			data, err := os.ReadFile(rest)
			if err != nil {
				panic(err)
			}
			var commands []string
			if err = json.Unmarshal(data, &commands); err != nil {
				panic(err)
			}
			mu.Lock()
			corners := append([]trialCorner(nil), observer.latest.Corners...)
			mu.Unlock()
			index := 0
			for _, corner := range corners {
				if corner.Glyph != "┌" {
					continue
				}
				if index >= len(commands) {
					panic("more panes than workload commands")
				}
				x, y := corner.X+1, corner.Y+1
				fmt.Fprintf(ptmx, "\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x, y, x, y)
				time.Sleep(30 * time.Millisecond)
				fmt.Fprint(ptmx, commands[index]+"\r")
				index++
				readyBy := time.Now().Add(15 * time.Second)
				for {
					mu.Lock()
					ready := false
					for _, pane := range observer.latest.Panes {
						if pane.ID == index {
							ready = true
						}
					}
					mu.Unlock()
					if ready {
						break
					}
					if time.Now().After(readyBy) {
						panic("pane command did not reach target")
					}
					time.Sleep(time.Millisecond)
				}
			}
			if index != len(commands) {
				panic("missing pane workload")
			}
		case "dragtrial":
			if observer == nil {
				panic("dragtrial needs -timeline")
			}
			settle()
			runDragTrial(rest, observer, ptmx, &mu)
		case "dragmeasure": // COUNT X Y DX DY WATCHX WATCHY: alternate one-cell movements
			var count, x, y, dx, dy, wx, wy int
			if n, _ := fmt.Sscanf(rest, "%d %d %d %d %d %d %d", &count, &x, &y, &dx, &dy, &wx, &wy); n != 7 {
				panic("invalid dragmeasure")
			}
			settle()
			mu.Lock()
			if x == 0 {
				for col := 27; col < *cols; col++ {
					if c := emu.CellAt(col-1, 1); c != nil && c.Content == "┐" {
						x = col
						wx = col
						break
					}
				}
			}
			if y == 0 {
				for row := 3; row < *rows; row++ {
					if c := emu.CellAt(26, row-1); c != nil && c.Content == "└" {
						y = row
						wy = row
						break
					}
				}
			}
			fmt.Printf("drag-geometry x=%d y=%d watch=%d,%d\n", x, y, wx, wy)
			glyph := emu.CellAt(wx-1, wy-1).Content
			mu.Unlock()
			cpu := func(label string) {
				for _, pid := range []int{cmd.Process.Pid, func() int { n, _ := strconv.Atoi(os.Getenv("PARITY_OBSERVE_PID")); return n }()} {
					out, _ := exec.Command("ps", "-o", "pid=,time=,%cpu=", "-p", strconv.Itoa(pid)).Output()
					fmt.Printf("cpu %s %s\n", label, strings.TrimSpace(string(out)))
				}
			}
			cpu("before")
			fmt.Fprintf(ptmx, "\x1b[<0;%d;%dM", x, y)
			time.Sleep(100 * time.Millisecond)
			for i := 0; i < count; i++ {
				off := (i + 1) % 2
				tx, ty := x+off*dx, y+off*dy
				sx, sy := wx+off*dx, wy+off*dy
				mu.Lock()
				begin := drained
				mu.Unlock()
				start := time.Now()
				fmt.Fprintf(ptmx, "\x1b[<32;%d;%dM", tx, ty)
				observed := int64(0)
				deadline := start.Add(250 * time.Millisecond)
				for time.Now().Before(deadline) {
					mu.Lock()
					cell := emu.CellAt(sx-1, sy-1)
					if observed == 0 && cell != nil && cell.Content == glyph && cellChanged[sy-1][sx-1] >= start.UnixMilli() && !emu.ModeSet(ansi.ModeSynchronizedOutput) {
						observed = max(cellChanged[sy-1][sx-1], completeAt)
					}
					mu.Unlock()
					time.Sleep(time.Millisecond)
				}
				mu.Lock()
				end := drained
				mu.Unlock()
				fmt.Printf("drag sample=%d sent_us=%d seen_ms=%d bytes=%d begin=%d end=%d target=%d,%d glyph=%q\n", i, start.UnixMicro(), observed, end-begin, begin, end, sx, sy, glyph)
			}
			fmt.Fprintf(ptmx, "\x1b[<0;%d;%dm", x, y)
			cpu("after")
		case "mouse", "mousetimed", "pixel": // kind, then one-based coordinates; mouse uses cells, pixel preserves subcell precision
			var kind string
			var x, y int
			if _, err := fmt.Sscanf(rest, "%s %d %d", &kind, &x, &y); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			m := uv.Mouse{X: x - 1, Y: y - 1, Button: uv.MouseLeft}
			if kind == "right-down" || kind == "right-up" {
				m.Button = uv.MouseRight
			}
			mu.Lock()
			if (op == "mouse" || op == "mousetimed") && emu.ModeSet(ansi.ModeMouseExtSgrPixel) {
				if *cellWidth <= 0 || *cellHeight <= 0 {
					mu.Unlock()
					fmt.Fprintln(os.Stderr, "pixel mouse needs cell size")
					os.Exit(1)
				}
				m.X = m.X**cellWidth + *cellWidth/2
				m.Y = m.Y**cellHeight + *cellHeight/2
			}
			if op == "mousetimed" {
				if observer == nil {
					panic("mousetimed requires timeline")
				}
				button := 0
				if kind == "right-down" || kind == "right-up" {
					button = 2
				}
				recordMouseInput(observer, ptmx, map[string]any{"type": "mouse.input", "kind": kind, "x": x, "y": y}, button, m.X+1, m.Y+1, kind == "up" || kind == "right-up")
				mu.Unlock()
				continue
			}
			switch kind {
			case "down", "right-down":
				emu.SendMouse(uv.MouseClickEvent(m))
			case "up", "right-up":
				emu.SendMouse(uv.MouseReleaseEvent(m))
			case "hover":
				m.Button = uv.MouseNone
				emu.SendMouse(uv.MouseMotionEvent(m))
			case "motion":
				emu.SendMouse(uv.MouseMotionEvent(m))
			case "wheel-up":
				m.Button = uv.MouseWheelUp
				emu.SendMouse(uv.MouseWheelEvent(m))
			case "wheel-down", "wheel-left", "wheel-right":
				m.Button = map[string]uv.MouseButton{"wheel-down": uv.MouseWheelDown, "wheel-left": uv.MouseWheelLeft, "wheel-right": uv.MouseWheelRight}[kind]
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
		case "clicktext", "locate", "clicklocated":
			label, err := strconv.Unquote(rest)
			if err != nil || label == "" {
				fmt.Fprintln(os.Stderr, "bad clicktext:", rest)
				os.Exit(1)
			}
			x, y, found := 0, 0, 0
			if op == "clicklocated" {
				if point, ok := locations[label]; ok {
					x, y, found = point[0], point[1], 1
				}
			} else {
				for row, line := range strings.Split(screen(), "\n") {
					if col := strings.Index(line, label); col >= 0 {
						x, y = ansi.StringWidth(line[:col])+1, row+1
						found++
					}
				}
			}
			if found != 1 {
				fmt.Fprintf(os.Stderr, "%s %q requires one located match, got %d\n", op, label, found)
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
				os.Exit(1)
			}
			if op == "locate" {
				locations[label] = [2]int{x, y}
				continue
			}
			mu.Lock()
			if emu.ModeSet(ansi.ModeMouseExtSgrPixel) {
				if *cellWidth <= 0 || *cellHeight <= 0 {
					mu.Unlock()
					fmt.Fprintln(os.Stderr, "pixel click needs cell size")
					os.Exit(1)
				}
				x, y = (x-1)**cellWidth+*cellWidth/2+1, (y-1)**cellHeight+*cellHeight/2+1
			}
			mu.Unlock()
			sentAt = time.Now()
			mark(op, label)
			_, _ = fmt.Fprintf(ptmx, "\x1b[<0;%d;%dM", x, y)
			if op == "clicktext" {
				time.Sleep(30 * time.Millisecond)
			}
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
				Cols, Rows        int
				Cells             [][]uv.Cell
				CapturedUnixMs    int64
				LastSendUnixMs    int64
				CellChangedUnixMs [][]int64 `json:",omitempty"`
			}{*cols, *rows, grid, time.Now().UnixMilli(), sentAt.UnixMilli(), cellChanged})
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
					_ = cmd.Process.Kill()
					_, _ = cmd.Process.Wait()
					os.Exit(1)
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
