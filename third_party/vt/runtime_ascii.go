package vt

import (
	"bytes"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

type runtimeRowState struct {
	end   int
	plain bool
}

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
		s.runtimeBlankBounds = make(map[*uv.Cell]runtimeRowState, len(s.buf.Lines))
	}
	clear(s.runtimeBlankBounds)
	for _, line := range s.buf.Lines {
		end := len(line)
		for end > 0 && line[end-1] == uv.EmptyCell {
			end--
		}
		if len(line) > 0 {
			s.runtimeBlankBounds[&line[0]] = runtimeRowState{end: end, plain: end == 0}
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
		canonical := len(line) > 0 && s.runtimeBlankBounds[&line[0]].end == 0
		if len(line) > 0 {
			key := &line[0]
			state := s.runtimeBlankBounds[key]
			state.end = max(state.end, n)
			if n > 0 {
				state.plain = canonical && s.cursorPen() == (uv.Style{}) && s.cursorLink() == (uv.Link{})
			}
			s.runtimeBlankBounds[key] = state
		}
		if canonical && n > 0 {
			pen, link := s.cursorPen(), s.cursorLink()
			plain := pen == (uv.Style{}) && link == (uv.Link{})
			first, last := n, 0
			if plain {
				for k, b := range rest[:n] {
					line[k].Content = asciiContent[b]
					if b != ansi.SP {
						first = min(first, k)
						last = k + 1
					}
				}
			} else {
				for k, b := range rest[:n] {
					line[k] = uv.Cell{Content: asciiContent[b], Width: 1, Style: pen, Link: link}
				}
				first, last = 0, n
			}
			if first < last {
				s.buf.TouchLine(first, s.cur.Y, last-first)
			}
			e.lastChar = rune(rest[n-1])
			s.setCursor(n, s.cur.Y, false)
		} else if n > 0 {
			e.printRun(rest[:n])
		}
		e.handleControl('\r')
		e.handleControl('\n')
		rest = rest[n+2:]
	}
	e.partial = e.partial[:0]
	return len(p), nil
}
