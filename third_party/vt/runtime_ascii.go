package vt

import (
	"bytes"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// WriteRuntimeASCII writes complete short printable ASCII CRLF rows while the
// runtime exclusively owns the emulator and its retained cell references. Its
// cursor callback must only record the final position; other Write callers keep
// their original per-movement notifications. No blank knowledge survives a call.
func (e *Emulator) WriteRuntimeASCII(p []byte) (int, error) {
	s := e.scr
	w := s.buf.Width()
	if e.closed.Load() || !e.canFastPrint() || e.atPhantom || len(e.grapheme) != 0 || s.cur.X != 0 || w <= 0 ||
		s.scroll != s.buf.Bounds() || s.cur.Pen.Bg != nil || s.scrollback == nil ||
		s.scrollback.OnDrop != nil || e.cb.Scroll != nil || e.logger != nil ||
		len(e.ccHandlers['\r']) != 1 || len(e.ccHandlers['\n']) != 1 ||
		uv.EmptyCell != (uv.Cell{Content: " ", Width: 1}) {
		return e.Write(p)
	}
	// Validate the entire span before enabling bounds: control handlers and
	// arbitrary partial input must never run while this knowledge is active.
	for rest := p; len(rest) > 0; {
		n := bytes.IndexByte(rest, '\r')
		if n < 0 || n >= w || n+1 >= len(rest) || rest[n+1] != '\n' {
			return e.Write(p)
		}
		for _, b := range rest[:n] {
			if b-ansi.SP >= ansi.DEL-ansi.SP {
				return e.Write(p)
			}
		}
		rest = rest[n+2:]
	}
	if s.runtimeBlankBounds == nil {
		s.runtimeBlankBounds = make(map[*uv.Cell]int, len(s.buf.Lines))
	}
	clear(s.runtimeBlankBounds)
	for _, line := range s.buf.Lines {
		end := len(line)
		for end > 0 && line[end-1] == uv.EmptyCell {
			end--
		}
		if len(line) > 0 {
			s.runtimeBlankBounds[&line[0]] = end
		}
	}
	old := s.cur.Position
	callback := e.cb.CursorPosition
	e.cb.CursorPosition = nil
	s.runtimeASCII = true
	defer func() {
		s.runtimeASCII = false
		clear(s.runtimeBlankBounds)
		e.cb.CursorPosition = callback
		if callback != nil && old != s.cur.Position {
			callback(old, s.cur.Position)
		}
	}()
	for rest := p; len(rest) > 0; {
		n := bytes.IndexByte(rest, '\r')
		line := s.buf.Line(s.cur.Y)
		if len(line) > 0 {
			key := &line[0]
			s.runtimeBlankBounds[key] = max(s.runtimeBlankBounds[key], n)
		}
		if _, err := e.Write(rest[:n+2]); err != nil {
			return len(p) - len(rest), err
		}
		rest = rest[n+2:]
	}
	return len(p), nil
}
