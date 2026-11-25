package sidebarplugin

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

type Position string

const (
	Left  Position = "left"
	Right Position = "right"

	metadataLines = 20
	metadataBytes = 64 << 10
)

func readMetadata(path string, item *Item) {
	// A file replaced by a named pipe during a scan must not block the runner.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		return
	}

	defaultPosition := item.Position
	remaining := &io.LimitedReader{R: file, N: metadataBytes + 1}
	reader := bufio.NewReader(remaining)
	for range metadataLines {
		line, err := reader.ReadString('\n')
		if err != nil && (err != io.EOF || remaining.N == 0) {
			break
		}
		line = strings.TrimRight(line, "\r\n")
		if !utf8.ValidString(line) || strings.ContainsFunc(line, func(r rune) bool {
			return r != '\t' && unicode.IsControl(r)
		}) {
			break
		}
		if comment, ok := strings.CutPrefix(strings.TrimSpace(line), "#"); ok {
			key, value, _ := strings.Cut(comment, "=")
			value = strings.TrimSpace(value)
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "ikesu.position":
				switch position := Position(strings.ToLower(value)); position {
				case Left, Right:
					item.Position = position
				default:
					item.Position = defaultPosition
				}
			case "ikesu.title":
				if value != "" {
					item.Name = value
				}
			case "ikesu.order":
				if n, err := strconv.Atoi(value); err == nil {
					item.Order = n
				}
			}
		}
		if err == io.EOF {
			break
		}
	}
}
