package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type dragTrial struct {
	Kind             string `json:"kind"`
	Path             string `json:"path"`
	Count            int    `json:"count"`
	Rate             int    `json:"rate"`
	Warmup           int    `json:"warmup"`
	Extent           int    `json:"extent"`
	ReleaseDifferent bool   `json:"release_different"`
}

func runDragTrial(path string, observer *timeline, out io.Writer, mu *sync.Mutex) {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	var trial dragTrial
	if err = json.Unmarshal(data, &trial); err != nil {
		panic(err)
	}
	if trial.Count <= 0 || trial.Rate <= 0 || trial.Warmup < 0 {
		panic("invalid trial counts/rate")
	}
	mu.Lock()
	frame := observer.latest
	mu.Unlock()
	x, y, wx, wy, dx, dy := 0, 10, 0, 0, 1, 0
	switch trial.Kind {
	case "vertical":
		for _, c := range frame.Corners {
			if c.Glyph == "┐" && c.Y == 2 {
				x, wx, wy = c.X, c.X, c.Y
				break
			}
		}
	case "horizontal":
		dx, dy = 0, 1
		x = 40
		for _, c := range frame.Corners {
			if c.Glyph == "└" && c.X == 27 {
				y, wx, wy = c.Y, c.X, c.Y
				break
			}
		}
	case "left":
		x, wx, wy = 26, 27, 2
	case "right":
		x, wx, wy = observer.cols-25, observer.cols-26, 2
		dx = -1
	default:
		panic("unsupported drag kind")
	}
	if x <= 0 || y <= 0 || wx <= 0 || wy <= 0 {
		panic("initial divider not found")
	}
	observer.record(map[string]any{"type": "trial", "clock_ns": monotonicNS(), "config": trial, "x": x, "y": y, "watch_x": wx, "watch_y": wy, "dx": dx, "dy": dy, "cols": observer.cols, "rows": observer.rows})
	send := func(button, px, py int, release bool) {
		suffix := "M"
		if release {
			suffix = "m"
		}
		if _, err := fmt.Fprintf(out, "\x1b[<%d;%d;%d%s", button, px, py, suffix); err != nil {
			panic(err)
		}
	}
	send(0, x, y, false)
	extent := trial.Extent
	if extent <= 0 {
		extent = 1
	}
	step := func(n int) int {
		period := 2 * extent
		m := (n + 1) % period
		if m > extent {
			m = period - m
		}
		return m
	}
	for i := 0; i < trial.Warmup; i++ {
		off := step(i)
		send(32, x+off*dx, y+off*dy, false)
		time.Sleep(time.Second / time.Duration(trial.Rate))
	}
	send(32, x, y, false)
	time.Sleep(250 * time.Millisecond)
	start := time.Now()
	for i := 0; i < trial.Count; i++ {
		time.Sleep(time.Until(start.Add(time.Duration(i) * time.Second / time.Duration(trial.Rate))))
		off := step(i)
		px, py := x+off*dx, y+off*dy
		observer.record(map[string]any{"type": "input", "clock_ns": monotonicNS(), "index": i, "x": px, "y": py, "watch_x": wx + off*dx, "watch_y": wy + off*dy})
		send(32, px, py, false)
	}
	time.Sleep(time.Until(start.Add(time.Duration(trial.Count) * time.Second / time.Duration(trial.Rate))))
	// A separate semantic case exercises a release coordinate different from
	// the last motion. Normal performance trials release at the last position.
	off := step(trial.Count - 1)
	if trial.ReleaseDifferent {
		if off == extent {
			off--
		} else {
			off++
		}
	}
	observer.record(map[string]any{"type": "release", "clock_ns": monotonicNS(), "x": x + off*dx, "y": y + off*dy, "watch_x": wx + off*dx, "watch_y": wy + off*dy})
	send(0, x+off*dx, y+off*dy, true)
	time.Sleep(2 * time.Second)
	observer.record(map[string]any{"type": "finish", "clock_ns": monotonicNS()})
}
