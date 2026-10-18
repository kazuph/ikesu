package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type captureChunk struct {
	End  int64 `json:"end"`
	Time int64 `json:"time_unix_ns"`
}

type measureWindow struct{ Start, End int64 }

func readWindow(root string) (measureWindow, error) {
	data, err := os.ReadFile(filepath.Join(root, "measurement.json"))
	if err != nil {
		return measureWindow{}, err
	}
	var saved struct {
		Events []struct {
			Kind, Label string
			Time        int64 `json:"time_ms"`
		}
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return measureWindow{}, err
	}
	var window measureWindow
	starts, ends := 0, 0
	for _, event := range saved.Events {
		if event.Kind == "mark" && event.Label == "scroll-start" {
			window.Start, starts = event.Time, starts+1
		}
		if event.Kind == "mark" && event.Label == "measurement-end" {
			window.End, ends = event.Time, ends+1
		}
	}
	if starts != 1 || ends != 1 || window.End < window.Start {
		return measureWindow{}, fmt.Errorf("measurement has no unique ordered scroll/switch window")
	}
	return window, nil
}

func readCaptureClock(file string) ([]captureChunk, error) {
	input, err := os.Open(strings.TrimSuffix(file, ".bin") + ".jsonl")
	if err != nil {
		return nil, err
	}
	defer input.Close()
	var chunks []captureChunk
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		var chunk captureChunk
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			return nil, err
		}
		if chunk.End <= 0 || chunk.Time <= 0 || len(chunks) > 0 && chunk.End <= chunks[len(chunks)-1].End {
			return nil, fmt.Errorf("invalid capture byte clock")
		}
		chunks = append(chunks, chunk)
	}
	return chunks, scanner.Err()
}

// Keep complete messages received by the window end. A window may end during
// a transmission; that unfinished message is excluded, never treated as an
// image the viewer has received. Its byte count remains in the report.
func windowPrefix(data []byte, chunks []captureChunk, end int64) ([]byte, int, error) {
	cutoff := int64(0)
	for _, chunk := range chunks {
		if chunk.End > int64(len(data)) {
			return nil, 0, fmt.Errorf("capture clock exceeds saved bytes")
		}
		if chunk.Time/1_000_000 <= end {
			cutoff = chunk.End
		}
	}
	data = data[:cutoff]
	complete := 0
	if bytes.HasPrefix(data, []byte("herdr-saved-ssh-bridge-ready\n")) {
		complete = len("herdr-saved-ssh-bridge-ready\n")
		for len(data)-complete >= 4 {
			size := int64(binary.LittleEndian.Uint32(data[complete:]))
			if size > int64(len(data)-complete-4) {
				break
			}
			complete += 4 + int(size)
		}
	} else {
		complete = bytes.LastIndexByte(data, '\n') + 1
	}
	return data[:complete], len(data) - complete, nil
}

func recordTime(row *record, chunks []captureChunk) error {
	index := sort.Search(len(chunks), func(i int) bool { return chunks[i].End >= row.WireEnd })
	if index == len(chunks) {
		return fmt.Errorf("image completion is outside the byte clock")
	}
	row.TimeUnixMs = chunks[index].Time / 1_000_000
	return nil
}
