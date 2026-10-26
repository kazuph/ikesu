package runtime

import "github.com/kazuph/ikesu/internal/benchstage"

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// rowRef records physical row boundaries as output reaches the emulator. The
// emulator owns cells, styles and links; ikesu owns automatic-wrap metadata.
type rowRef struct {
	continued bool
	width     int
}
type readBuffer struct {
	main, alternate      []rowRef
	history              []rowRef
	byteHistory          bool
	historyCount         int
	historyIndexed       bool
	historyBase          *vt.ReflowSnapshot
	baseFirst, baseCount int
	start                int
	altHistory           []rowRef
	altStart             int
	pending              bool
	state                byte
	kind                 byte
	parser               *ansi.Parser
	utf8Pending          []byte
	partial              []byte
	top, bottom          int
	left, right          int
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
func (r *readBuffer) pushHistory(alt bool, row rowRef) {
	if !alt && (r.byteHistory || r.historyIndexed) {
		r.pushByteHistory(row)
		if !r.byteHistory && r.historyCount > ScrollbackSize {
			r.dropHistory()
		}
		return
	}
	history, start := &r.history, &r.start
	limit := ScrollbackSize
	if alt {
		history, start = &r.altHistory, &r.altStart
		limit = vt.DefaultScrollbackSize
	}
	if len(*history) < limit {
		*history = append(*history, row)
		return
	}
	(*history)[*start] = row
	*start = (*start + 1) % limit
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
		if history {
			for _, row := range rows[top : top+n] {
				if row.width == 0 {
					row.width = w
				}
				r.pushHistory(alt, row)
			}
		}
		copy(rows[top:bottom-n], rows[top+n:bottom])
		clear(rows[bottom-n : bottom])
	} else {
		copy(rows[top+n:bottom], rows[top:bottom-n])
		clear(rows[top : top+n])
	}
}
func (r *readBuffer) resizeRows(alt bool, oldWidth, height, pushed, pulled int) {
	rows := r.rows(alt)
	for _, row := range rows[:pushed] {
		if row.width == 0 {
			row.width = oldWidth
		}
		r.pushHistory(alt, row)
	}
	next := make([]rowRef, height)
	copy(next[pulled:], rows[pushed:])
	history, start := &r.history, &r.start
	if alt {
		history, start = &r.altHistory, &r.altStart
	}
	for y := pulled - 1; y >= 0; y-- {
		if len(*history) == 0 {
			break
		}
		last := (*start + len(*history) - 1) % len(*history)
		next[y] = (*history)[last]
		// Linearize only when resize pulls rows from a wrapped ring.
		if *start != 0 {
			ordered := append(append([]rowRef{}, (*history)[*start:]...), (*history)[:*start]...)
			*history, *start = ordered, 0
		}
		*history = (*history)[:len(*history)-1]
	}
	if alt {
		r.alternate = next
	} else {
		r.main = next
	}
}
func (r *readBuffer) linefeed(p *Pane, continued bool) {
	pos := p.emu.CursorPosition()
	r.linefeedAt(p, pos, continued)
}
func (r *readBuffer) linefeedAt(p *Pane, pos uv.Position, continued bool) {
	rows := r.rows(p.altScreen)
	if pos.Y == r.bottom-1 && pos.X >= r.left && pos.X < r.right {
		r.shift(p.altScreen, r.top, r.bottom, 1, true, r.left == 0 && r.right == p.emu.Width(), p.emu.Width())
		rows[r.bottom-1].continued = continued
	} else if pos.Y+1 < len(rows) {
		rows[pos.Y+1].continued = continued
	}
}
func (r *readBuffer) resize(w, h int) {
	defer benchstage.Span("runtime.readresize")()
	for _, target := range []*[]rowRef{&r.main, &r.alternate} {
		next := make([]rowRef, h)
		copy(next, *target)
		*target = next
	}
	if r.right != w {
		for y := range r.alternate {
			r.alternate[y].width = 0
		}
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
			r.historyCount = 0
			r.historyBase, r.baseFirst, r.baseCount, r.historyIndexed = nil, 0, 0, false
			r.start = 0
			r.altHistory = nil
			r.altStart = 0
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
			for y, row := range rows {
				if !emptyRow(p.emu, y) {
					row.width = p.emu.Width()
					r.pushHistory(p.altScreen, row)
				}
			}
			clear(rows)
		} else if a == 3 {
			r.history = nil
			r.historyCount = 0
			r.historyBase, r.baseFirst, r.baseCount, r.historyIndexed = nil, 0, 0, false
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

// Complete short CRLF rows cannot wrap or change terminal modes. Record only
// their line boundaries and let the emulator parse the whole span once, rather
// than entering both parsers and Write separately for each CR and LF.
func (p *Pane) writeShortLines(chunk []byte) int {
	r := p.read
	pos := p.emu.CursorPosition()
	w := p.emu.Width()
	if r.state != ansi.NormalState || r.pending || pos.X != 0 || r.left != 0 || r.right != w {
		return 0
	}
	end, lines := 0, 0
	for i := 0; i < len(chunk); {
		span := chunk[i : i+min(w, len(chunk)-i)]
		n := bytes.IndexByte(span, '\r')
		if n < 0 || i+n+1 >= len(chunk) || chunk[i+n+1] != '\n' {
			break
		}
		valid := true
		for _, b := range span[:n] {
			if b-ansi.SP >= ansi.DEL-ansi.SP {
				valid = false
				break
			}
		}
		if !valid {
			break
		}
		i += n + 2
		end, lines = i, lines+1
	}
	if end == 0 {
		return 0
	}
	for i := 0; i < lines; i++ {
		r.linefeedAt(p, pos, false)
		if pos.Y != r.bottom-1 {
			pos.Y = min(pos.Y+1, p.emu.Height()-1)
		}
	}
	if r.byteHistory && !p.altScreen {
		// This pane owns the drop callback, which only prunes row metadata.
		// Short CRLF rows cannot invoke a mode/resize callback that reads it,
		// so prune once after Write while the caller still holds p.mu.
		sb := p.emu.Scrollback()
		callback := sb.OnDrop
		sb.OnDrop = nil
		n, err := p.emu.WriteRuntimeASCII(chunk[:end])
		sb.OnDrop = callback
		if n == end && err == nil {
			for r.historyCount > sb.Len() {
				r.dropHistory()
			}
		}
	} else {
		_, _ = p.emu.WriteRuntimeASCII(chunk[:end])
	}
	return end
}

func (p *Pane) writeRecorded(chunk []byte) {
	defer benchstage.Span("runtime.recorded")()
	r := p.read
	if r == nil {
		r = newReadBuffer(p.emu.Width(), p.emu.Height())
		p.read = r
		if p.emu.Scrollback().ByteLimited() {
			r.byteHistory = true
			p.emu.Scrollback().OnDrop = r.dropHistory
		}
	}
	defer func() {
		defer benchstage.Span("runtime.prepareindex")()
		history := r.historyLen()
		p.emu.PrepareReflowHistory(func(y int) vt.ReflowRow {
			var ref rowRef
			if y < history {
				ref = r.historyRef(y)
			} else {
				ref = r.main[0]
			}
			return vt.ReflowRow{Continued: ref.continued, Width: ref.width}
		})
	}()
	if len(r.utf8Pending) > 0 {
		chunk = append(r.utf8Pending, chunk...)
		r.utf8Pending = nil
	}
	for len(chunk) > 0 {
		if n := p.writeShortLines(chunk); n > 0 {
			chunk = chunk[n:]
			continue
		}
		if r.state == ansi.NormalState && chunk[0] >= 0xc0 && !utf8.FullRune(chunk) {
			r.utf8Pending = append(r.utf8Pending[:0], chunk...)
			break
		}
		if r.state == ansi.NormalState && chunk[0]-ansi.SP < ansi.DEL-ansi.SP {
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
			for n < limit && chunk[n]-ansi.SP < ansi.DEL-ansi.SP {
				n++
			}
			_, _ = p.emu.Write(chunk[:n])
			r.rows(p.altScreen)[p.emu.CursorPosition().Y].width = 0
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
		if oldState == ansi.NormalState {
			r.partial = r.partial[:0]
		}
		if state != ansi.NormalState {
			r.partial = append(r.partial, chunk[:n]...)
		} else {
			r.partial = r.partial[:0]
		}
		printable := width > 0 || oldState == ansi.NormalState && chunk[0] >= 0xc0
		if printable {
			pos := p.emu.CursorPosition()
			x := pos.X
			if p.emu.ModeSet(ansi.ModeAutoWrap) && (r.pending || x+width > p.emu.Width()) {
				if !r.pending {
					r.rows(p.altScreen)[pos.Y].width = x
				}
				r.linefeed(p, true)
				x = 0
			}
			_, _ = p.emu.Write(seq)
			r.rows(p.altScreen)[p.emu.CursorPosition().Y].width = 0
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
	return p.readLinesLocked(source, strip), nil
}

// DetectionLines includes the SPEC 工程6 24 rows above the current screen, independent of user scrolling.
func (p *Pane) DetectionLines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.readLinesLocked("detection", true)
}

// DetectionScreenLines unwraps the live screen without including older prompts.
func (p *Pane) DetectionScreenLines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.readLinesLocked("screen_detection", true)
}

func (p *Pane) readLinesLocked(source string, strip bool) []string {
	logical := source == "recent_unwrapped" || source == "recent-unwrapped" || source == "detection" || source == "screen_detection"
	refs := []rowRef{}
	rows := []uv.Line{}
	if source != "visible" && source != "screen_detection" && !p.altScreen {
		start := 0
		if source == "detection" {
			start = max(0, p.emu.ScrollbackLen()-24)
		}
		for y := start; y < p.emu.ScrollbackLen(); y++ {
			rows = append(rows, p.emu.Scrollback().Line(y))
			ref := rowRef{}
			if p.read != nil {
				ref = p.read.historyRef(y)
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
			recorded := p.read.rows(p.altScreen)[y]
			ref.continued = recorded.continued
			if recorded.width > 0 {
				ref.width = recorded.width
			}
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
	return out
}
func renderReadRow(row uv.Line, strip, full bool, width int) string {
	end := len(row)
	if full && width > 0 {
		end = min(end, width)
	}
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
