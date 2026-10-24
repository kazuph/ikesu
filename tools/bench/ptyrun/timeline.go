package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

type trialCorner struct {
	X, Y  int
	Glyph string
}
type trialPane struct {
	ID, Seq, Width, Height    int
	LayoutWidth, LayoutHeight int
	X, Y                      int
}
type trialFrame struct {
	Type    string        `json:"type"`
	At      int64         `json:"clock_ns"`
	Bytes   int64         `json:"bytes"`
	Corners []trialCorner `json:"corners"`
	Panes   []trialPane   `json:"panes"`
}
type timeline struct {
	mu         sync.Mutex
	file       *os.File
	log        *json.Encoder
	emu        *vt.Emulator
	cols, rows int
	total      int64
	tail       []byte
	latest     trialFrame
	closed     bool
	receiver   kittyReceiver
	reply      io.Writer
}

var trialMarker = regexp.MustCompile(`@([0-9a-f])([0-9a-f]{4})([0-9a-f]{2})([0-9a-f]{2})!`)

func newTimeline(path string, emu *vt.Emulator, cols, rows int) *timeline {
	if path == "" {
		return nil
	}
	file, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	t := &timeline{file: file, log: json.NewEncoder(file), emu: emu, cols: cols, rows: rows}
	t.receiver = kittyReceiver{record: t.record, clock: monotonicNS}
	emu.RegisterApcHandler(t.receiver.accept)
	emu.RegisterCsiHandler('n', func(params ansi.Params) bool {
		op, _, _ := params.Param(0, 0)
		if op != 5 {
			return false
		}
		if len(t.receiver.pending) != 0 {
			panic("status query before image payload completed")
		}
		if t.reply != nil {
			fmt.Fprint(t.reply, "\x1b[0n")
		}
		t.record(map[string]any{"type": "terminal.status_complete", "clock_ns": monotonicNS()})
		return true
	})
	return t
}
func (t *timeline) record(v any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	if err := t.log.Encode(v); err != nil {
		panic(err)
	}
}
func (t *timeline) feed(data []byte, at int64) {
	// The terminal parser can receive a split escape sequence. Carry only the
	// delimiter detector across chunks; never hold terminal query bytes back.
	end := []byte("\x1b[?2026l")
	combined := append(append([]byte(nil), t.tail...), data...)
	offset, previous := len(t.tail), 0
	for from := 0; from < len(combined); {
		n := bytes.Index(combined[from:], end)
		if n < 0 {
			break
		}
		to := from + n + len(end)
		from = to
		cut := to - offset
		if cut <= 0 {
			continue
		}
		_, _ = t.emu.Write(data[previous:cut])
		t.total += int64(cut - previous)
		previous = cut
		t.capture(at)
	}
	if previous < len(data) {
		_, _ = t.emu.Write(data[previous:])
		t.total += int64(len(data) - previous)
	}
	if !t.emu.ModeSet(ansi.ModeSynchronizedOutput) {
		t.capture(at)
	}
	t.tail = append(t.tail[:0], combined[max(0, len(combined)-len(end)+1):]...)
}
func (t *timeline) capture(at int64) {
	frame := trialFrame{Type: "frame", At: at, Bytes: t.total}
	lines := make([][]string, t.rows)
	for y := 0; y < t.rows; y++ {
		lines[y] = make([]string, t.cols)
		for x := 0; x < t.cols; x++ {
			c := t.emu.CellAt(x, y)
			if c == nil {
				continue
			}
			lines[y][x] = c.Content
			if strings.Contains("┌┐└┘", c.Content) && c.Content != "" {
				frame.Corners = append(frame.Corners, trialCorner{x + 1, y + 1, c.Content})
			}
		}
	}
	for _, corner := range frame.Corners {
		if corner.Glyph != "┌" {
			continue
		}
		left, top := corner.X-1, corner.Y-1
		right, bottom := -1, -1
		for x := left + 1; x < t.cols; x++ {
			if lines[top][x] == "┐" {
				right = x
				break
			}
		}
		for y := top + 1; y < t.rows; y++ {
			if lines[y][left] == "└" {
				bottom = y
				break
			}
		}
		if right < 0 || bottom < 0 {
			continue
		}
		var body strings.Builder
		for y := top + 1; y < bottom; y++ {
			for x := left + 1; x < right; x++ {
				c := lines[y][x]
				if c != " " {
					body.WriteString(c)
				}
			}
		}
		match := trialMarker.FindStringSubmatch(body.String())
		if match == nil {
			continue
		}
		values := make([]int, 4)
		for i := range values {
			n, _ := strconv.ParseInt(match[i+1], 16, 32)
			values[i] = int(n)
		}
		frame.Panes = append(frame.Panes, trialPane{ID: values[0], Seq: values[1], Width: values[2], Height: values[3], X: left + 2, Y: top + 2, LayoutWidth: right - left - 1 - func() int {
			if right-left-1 >= 5 {
				return 1
			}
			return 0
		}(), LayoutHeight: bottom - top - 1})
	}
	t.latest = frame
	t.record(frame)
}
func (t *timeline) close() {
	if t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.closed = true
		if err := t.file.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
}
