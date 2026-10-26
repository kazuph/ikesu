//go:build flatcells

package vt

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func (e *Emulator) WriteRuntimeASCII(p []byte) (int, error) {
	s := e.scr
	w := s.buf.Width()
	if e.closed.Load() || !e.canFastPrint() || e.atPhantom || len(e.grapheme) != 0 || s.cur.X != 0 || w <= 0 ||
		s.scroll != s.buf.Bounds() || s.cur.Pen.Bg != nil || s.scrollback == nil ||
		s.scrollback.OnDrop != nil || e.cb.Scroll != nil || e.logger != nil ||
		len(e.ccHandlers['\r']) != 1 || len(e.ccHandlers['\n']) != 1 || uv.EmptyCell != (uv.Cell{Content: " ", Width: 1}) {
		return e.Write(p)
	}
	for rest := p; len(rest) > 0; {
		n := bytes.IndexByte(rest, '\r')
		if n < 0 || n >= w || n+1 >= len(rest) || rest[n+1] != '\n' {
			return e.Write(p)
		}
		for _, value := range rest[:n] {
			if value-ansi.SP >= ansi.DEL-ansi.SP {
				return e.Write(p)
			}
		}
		rest = rest[n+2:]
	}
	if cap(s.runtimeBlankBounds) < s.buf.Height() {
		s.runtimeBlankBounds = make([]runtimeRowState, s.buf.Height())
	} else {
		s.runtimeBlankBounds = s.runtimeBlankBounds[:s.buf.Height()]
	}
	s.runtimeRowHead = 0
	s.runtimeScrollTouched = false
	empty := FlatCell{Text: ' ' + 1, Width: 1}
	for y := 0; y < s.buf.Height(); y++ {
		end := max(s.buf.rowLength(y), 0)
		for end > 0 && s.buf.raw(end-1, y) == empty {
			end--
		}
		s.runtimeBlankBounds[y] = runtimeRowState{end: end, plain: end == 0}
	}
	old := s.cur.Position
	callback := e.cb.CursorPosition
	e.cb.CursorPosition = nil
	s.runtimeASCII = true
	defer func() {
		s.runtimeASCII = false
		s.runtimeScrollTouched = false
		clear(s.runtimeBlankBounds)
		s.runtimeBlankBounds = s.runtimeBlankBounds[:0]
		s.runtimeRowHead = 0
		e.cb.CursorPosition = callback
		if callback != nil && old != s.cur.Position {
			callback(old, s.cur.Position)
		}
	}()
	pen, link := s.cursorPen(), s.cursorLink()
	plain := pen == (uv.Style{}) && link == (uv.Link{})
	for rest := p; len(rest) > 0; {
		n := bytes.IndexByte(rest, '\r')
		canonical := false
		if s.buf.rowLength(s.cur.Y) > 0 {
			idx := (s.runtimeRowHead + s.cur.Y) % len(s.runtimeBlankBounds)
			state := &s.runtimeBlankBounds[idx]
			canonical = state.end == 0
			state.end = max(state.end, n)
			if n > 0 {
				state.plain = canonical && plain
				state.text = nil
				if state.plain {
					state.text = rest[:n]
				}
			}
		}
		if canonical && n > 0 {
			s.buf.writeASCII(0, s.cur.Y, rest[:n], pen, link)
			e.lastChar = rune(rest[n-1])
			s.setCursor(n, s.cur.Y, false)
		} else if n > 0 {
			e.printRun(rest[:n])
		}
		e.flushGrapheme()
		e.carriageReturn()
		e.linefeed()
		rest = rest[n+2:]
	}
	e.partial = e.partial[:0]
	return len(p), nil
}
