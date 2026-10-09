package runtime

import (
	"fmt"
	"strings"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// rowRef records physical row boundaries as output reaches the emulator. The
// emulator owns cells, styles and links; gtx owns automatic-wrap metadata.
type rowRef struct {
	continued bool
	width     int
}
type readBuffer struct {
	main, alternate []rowRef
	history         []rowRef
	start           int
	pending         bool
	state           byte
	kind            byte
	parser          *ansi.Parser
	utf8Pending     []byte
	top, bottom     int
	left, right     int
}

func newReadBuffer(w, h int) *readBuffer {
	return &readBuffer{main: make([]rowRef, h), alternate: make([]rowRef, h), parser: ansi.NewParser(), bottom: h, right: w}
}
func (r *readBuffer) rows(alt bool) []rowRef {
	if alt {
		return r.alternate
	}
	return r.main
}
func (r *readBuffer) push(row rowRef) {
	if len(r.history) < ScrollbackSize {
		r.history = append(r.history, row)
		return
	}
	r.history[r.start] = row
	r.start = (r.start + 1) % ScrollbackSize
}
func (r *readBuffer) shift(alt bool, top, bottom, n int, up, history bool, w int) {
	rows := r.rows(alt)
	top = max(0, top)
	bottom = min(len(rows), bottom)
	n = min(max(0, n), bottom-top)
	if n <= 0 {
		return
	}
	if up {
		if history && !alt {
			for _, row := range rows[top : top+n] {
				row.width = w
				r.push(row)
			}
		}
		copy(rows[top:bottom-n], rows[top+n:bottom])
		clear(rows[bottom-n : bottom])
	} else {
		copy(rows[top+n:bottom], rows[top:bottom-n])
		clear(rows[top : top+n])
	}
}
func (r *readBuffer) linefeed(p *Pane, continued bool) {
	pos := p.emu.CursorPosition()
	rows := r.rows(p.altScreen)
	if pos.Y == r.bottom-1 && pos.X >= r.left && pos.X < r.right {
		r.shift(p.altScreen, r.top, r.bottom, 1, true, r.left == 0 && r.right == p.emu.Width(), p.emu.Width())
		rows[r.bottom-1].continued = continued
	} else if pos.Y+1 < len(rows) {
		rows[pos.Y+1].continued = continued
	}
}
func (r *readBuffer) resize(w, h int) {
	for _, target := range []*[]rowRef{&r.main, &r.alternate} {
		next := make([]rowRef, h)
		copy(next, *target)
		*target = next
	}
	r.top, r.bottom, r.left, r.right = 0, h, 0, w
	r.pending = false
}
func (r *readBuffer) control(p *Pane, seq []byte) {
	pos := p.emu.CursorPosition()
	rows := r.rows(p.altScreen)
	if r.kind == 0 && len(seq) == 1 {
		switch seq[0] {
		case '\n', '\v', '\f':
			r.linefeed(p, false)
			r.pending = false
		case '\r', '\b':
			r.pending = false
		}
		return
	}
	if r.kind != '[' {
		if r.kind == 'D' || r.kind == 'E' {
			r.linefeed(p, false)
			r.pending = false
		}
		if r.kind == 'M' && pos.Y == r.top {
			r.shift(p.altScreen, r.top, r.bottom, 1, false, false, p.emu.Width())
		}
		if r.kind == 'c' {
			clear(r.main)
			clear(r.alternate)
			r.history = nil
			r.start = 0
			r.resize(p.emu.Width(), p.emu.Height())
		}
		return
	}
	cmd := ansi.Cmd(r.parser.Command())
	if cmd.Prefix() != 0 || cmd.Intermediate() != 0 {
		return
	}
	a, _ := r.parser.Param(0, 0)
	n := max(1, a)
	switch cmd.Final() {
	case 'r':
		b, _ := r.parser.Param(1, p.emu.Height())
		top := max(0, a-1)
		if top < b && b <= p.emu.Height() {
			r.top, r.bottom = top, b
			r.pending = false
		}
	case 's':
		if p.emu.ModeSet(ansi.ModeLeftRightMargin) {
			b, _ := r.parser.Param(1, p.emu.Width())
			if a >= 1 && a < b && b <= p.emu.Width() {
				r.left, r.right = a-1, b
			}
		}
	case 'J':
		if a == 2 {
			if !p.altScreen {
				for y, row := range rows {
					if !emptyRow(p.emu, y) {
						row.width = p.emu.Width()
						r.push(row)
					}
				}
			}
			clear(rows)
		} else if a == 3 {
			r.history = nil
			r.start = 0
			clear(rows)
		} else if a == 0 {
			clear(rows[pos.Y+1:])
		} else if a == 1 {
			clear(rows[:pos.Y])
		}
	case 'K':
		if a == 2 || a == 1 && pos.X == p.emu.Width()-1 || a == 0 && pos.X == 0 {
			rows[pos.Y].continued = false
		}
	case 'L', 'M':
		if pos.Y >= r.top && pos.Y < r.bottom && pos.X >= r.left && pos.X < r.right {
			r.shift(p.altScreen, pos.Y, r.bottom, n, cmd.Final() == 'M', cmd.Final() == 'M' && pos.Y == r.top && r.left == 0 && r.right == p.emu.Width(), p.emu.Width())
		}
	case 'S', 'T':
		r.shift(p.altScreen, r.top, r.bottom, n, cmd.Final() == 'S', cmd.Final() == 'S' && r.left == 0 && r.right == p.emu.Width(), p.emu.Width())
	}
	if strings.ContainsRune("ABCDEFGH IJKLMPSTX`adefrsu", rune(cmd.Final())) {
		r.pending = false
	}
}
func emptyRow(e *vt.Emulator, y int) bool {
	for x := 0; x < e.Width(); x++ {
		c := e.CellAt(x, y)
		if c != nil && c.Content != "" && c.Content != " " {
			return false
		}
	}
	return true
}
func (p *Pane) writeRecorded(chunk []byte) {
	r := p.read
	if r == nil {
		r = newReadBuffer(p.emu.Width(), p.emu.Height())
		p.read = r
	}
	if len(r.utf8Pending) > 0 {
		chunk = append(r.utf8Pending, chunk...)
		r.utf8Pending = nil
	}
	for len(chunk) > 0 {
		if r.state == ansi.NormalState && chunk[0] >= 0xc0 && !utf8.FullRune(chunk) {
			r.utf8Pending = append(r.utf8Pending[:0], chunk...)
			break
		}
		if r.state == ansi.NormalState && chunk[0] >= 32 && chunk[0] < 127 {
			if r.pending && p.emu.ModeSet(ansi.ModeAutoWrap) {
				r.linefeed(p, true)
			}
			pos := p.emu.CursorPosition()
			x := pos.X
			if r.pending && p.emu.ModeSet(ansi.ModeAutoWrap) {
				x = 0
			}
			limit := min(len(chunk), p.emu.Width()-x)
			n := 0
			for n < limit && chunk[n] >= 32 && chunk[n] < 127 {
				n++
			}
			_, _ = p.emu.Write(chunk[:n])
			r.pending = p.emu.ModeSet(ansi.ModeAutoWrap) && x+n >= p.emu.Width()
			chunk = chunk[n:]
			continue
		}
		oldState := r.state
		if oldState == ansi.NormalState {
			r.kind = 0
			if chunk[0] == 27 && len(chunk) > 1 {
				r.kind = chunk[1]
			} else if chunk[0] == ansi.CSI {
				r.kind = '['
			}
		} else if oldState == ansi.EscapeState && r.kind == 0 {
			r.kind = chunk[0]
		}
		seq, width, n, state := ansi.DecodeSequence(chunk, r.state, r.parser)
		if n <= 0 {
			n = 1
			seq = chunk[:1]
		}
		r.state = state
		printable := width > 0 || oldState == ansi.NormalState && chunk[0] >= 0xc0
		if printable {
			if r.pending && p.emu.ModeSet(ansi.ModeAutoWrap) {
				r.linefeed(p, true)
			}
			x := p.emu.CursorPosition().X
			if r.pending && p.emu.ModeSet(ansi.ModeAutoWrap) {
				x = 0
			}
			_, _ = p.emu.Write(seq)
			r.pending = p.emu.ModeSet(ansi.ModeAutoWrap) && x+width >= p.emu.Width()
		} else {
			if state == ansi.NormalState {
				r.control(p, seq)
			}
			_, _ = p.emu.Write(seq)
		}
		chunk = chunk[n:]
	}
}

// ReadLines returns rendered rows, optionally retaining their ANSI style and
// hyperlink information. Logical rows join only recorded automatic wraps.
func (p *Pane) ReadLines(source string, strip bool) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if source != "visible" && source != "recent" && source != "recent_unwrapped" && source != "recent-unwrapped" {
		return nil, fmt.Errorf("read source %q is not supported in this stage", source)
	}
	logical := source == "recent_unwrapped" || source == "recent-unwrapped"
	refs := []rowRef{}
	rows := []uv.Line{}
	if source != "visible" && !p.altScreen {
		for y := 0; y < p.emu.ScrollbackLen(); y++ {
			rows = append(rows, p.emu.Scrollback().Line(y))
			ref := rowRef{}
			if p.read != nil && y < len(p.read.history) {
				ref = p.read.history[(p.read.start+y)%len(p.read.history)]
			}
			refs = append(refs, ref)
		}
	}
	for y := 0; y < p.emu.Height(); y++ {
		line := make(uv.Line, p.emu.Width())
		for x := range line {
			if c := p.emu.CellAt(x, y); c != nil {
				line[x] = *c
			}
		}
		rows = append(rows, line)
		ref := rowRef{width: p.emu.Width()}
		if p.read != nil {
			ref.continued = p.read.rows(p.altScreen)[y].continued
		}
		refs = append(refs, ref)
	}
	out := []string{}
	for y, row := range rows {
		continueNext := logical && y+1 < len(refs) && refs[y+1].continued
		text := renderReadRow(row, strip, continueNext, refs[y].width)
		if logical && refs[y].continued && len(out) > 0 {
			out[len(out)-1] += text
		} else {
			out = append(out, text)
		}
	}
	return out, nil
}
func renderReadRow(row uv.Line, strip, full bool, width int) string {
	end := len(row)
	if !full {
		for end > 0 && (row[end-1].Content == "" || row[end-1].Content == " ") {
			end--
		}
	}
	var b strings.Builder
	var style uv.Style
	var link uv.Link
	used := 0
	for _, c := range row[:end] {
		if c.Width == 0 {
			continue
		}
		used += c.Width
		if !strip {
			if !c.Style.Equal(&style) {
				b.WriteString(c.Style.String())
				style = c.Style
			}
			if c.Link != link {
				b.WriteString(ansi.SetHyperlink(c.Link.URL, c.Link.Params))
				link = c.Link
			}
		}
		if c.Content == "" {
			b.WriteByte(' ')
		} else {
			b.WriteString(c.Content)
		}
	}
	if full && width > used {
		b.WriteString(strings.Repeat(" ", width-used))
	}
	if !strip {
		if !style.IsZero() {
			b.WriteString(ansi.ResetStyle)
		}
		if !link.IsZero() {
			b.WriteString(ansi.SetHyperlink("", ""))
		}
	}
	return b.String()
}
