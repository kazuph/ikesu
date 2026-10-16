// Package input frames raw terminal input bytes into events. It buffers
// incomplete escape sequences across reads and discards split mouse-sequence
// fragments so that they can never leak into a pane as text.
package input

import (
	"bytes"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
)

// IdleFlushMs is how long the framer waits for the tail of a sequence before
// treating buffered bytes as complete input.
const IdleFlushMs = 10

// sgrTailCap bounds how many bytes the orphaned-SGR-tail discards.
const sgrTailCap = 32

// Kind classifies decoded input events.
type Kind int

const (
	// KindKey is a key press. Key holds the decoded key.
	KindKey Kind = iota
	// KindMouseClick / KindMouseRelease / KindMouseWheel / KindMouseMotion
	// are mouse events; Mouse holds the decoded struct.
	KindMouseClick
	KindMouseRelease
	KindMouseWheel
	KindMouseMotion
	// KindPaste is bracketed-paste content. Text holds the pasted bytes.
	KindPaste
	// KindFocus / KindBlur come from focus event reporting (1004).
	KindFocus
	KindBlur
	// KindUnknown covers every other sequence. It is never forwarded to a
	// pane; Raw keeps the bytes for diagnostics.
	KindUnknown
	KindTerminalReport
)

// Event is one framed input event.
type Event struct {
	Kind   Kind
	Raw    []byte
	Key    uv.Key
	Mouse  uv.Mouse
	Text   string
	Report uv.Event
}

// Framer buffers terminal input and emits complete events.
type Framer struct {
	buf []byte
	dec uv.EventDecoder

	// paste collects bracketed-paste payload bytes until CSI 201~.
	paste    bool
	pasteBuf []byte

	// escHeld means a lone ESC is being held one extra flush window so a
	// following "[<digits;...M" orphan tail can be discarded together with it.
	escHeld bool

	// tail drops SGR-mouse tail bytes (digits, ';', final 'M'/'m') that
	// arrived after an incomplete SGR sequence was discarded.
	tail    bool
	tailLen int
}

// Push feeds freshly read bytes and returns every complete event. When the
// returned Pending is true the caller must arm an idle timer (IdleFlushMs)
// and call FlushTimeout if no further input arrives.
func (f *Framer) Push(b []byte) []Event {
	if len(b) == 0 {
		return nil
	}
	// An orphaned SGR tail carries no ESC; drop it before it becomes text.
	b = f.dropTail(b)
	if len(b) == 0 {
		return nil
	}
	f.buf = append(f.buf, b...)
	return f.drain(false)
}

// FlushTimeout is called when input has been idle for IdleFlushMs while
// Pending was true. It resolves held bytes: incomplete SGR mouse fragments
// are discarded, a held lone ESC becomes an Escape key unless a probable
// tail is on its way.
func (f *Framer) FlushTimeout() []Event {
	if f.paste {
		return nil
	}
	if len(f.buf) == 0 {
		return nil
	}
	if isIncompleteSGRMouse(f.buf) {
		// A mouse sequence split across reads; discard the fragment and any
		// digits/; tail + terminator that arrive next. A held lone ESC is
		// inside f.buf and goes away with it.
		f.buf = f.buf[:0]
		f.escHeld = false
		f.tail = true
		f.tailLen = 0
		return nil
	}
	if f.escHeld {
		// Second idle window with the lone ESC still alone: it is a real
		// Escape keypress.
		f.escHeld = false
		f.buf = f.buf[:0]
		return []Event{{Kind: KindKey, Raw: []byte{0x1b}, Key: uv.Key{Code: uv.KeyEscape}}}
	}
	if len(f.buf) == 1 && f.buf[0] == 0x1b {
		// Hold the lone ESC one more window to catch an orphan tail.
		f.escHeld = true
		return nil
	}
	return f.drain(true)
}

// Pending reports whether buffered bytes await more input or a flush.
func (f *Framer) Pending() bool {
	return len(f.buf) > 0 || f.paste || f.escHeld
}

// drain decodes as many complete events as possible from f.buf. At timeout
// (forced=true) incomplete non-SGR sequences are decoded as-is so unknown
// output drops through KindUnknown rather than stalling forever.
func (f *Framer) drain(forced bool) []Event {
	var out []Event
	for len(f.buf) > 0 {
		// Held ESC plus a fresh SGR tail: discard both entirely.
		if f.escHeld {
			if len(f.buf) >= 2 && f.buf[0] == 0x1b && f.buf[1] == '[' {
				// Need at least "[<" to decide.
				if len(f.buf) >= 3 && f.buf[2] == '<' {
					if isIncompleteSGRMouse(f.buf) {
						break // wait for the rest / flush discards it
					}
					// Complete SGR mouse seq: drop the held ESC and emit.
					f.escHeld = false
					continue
				}
				if len(f.buf) < 3 {
					break // wait for the third byte
				}
			}
			// Held ESC followed by something else: emit Escape and continue.
			f.escHeld = false
			out = append(out, Event{Kind: KindKey, Raw: []byte{0x1b}, Key: uv.Key{Code: uv.KeyEscape}})
			f.buf = f.buf[1:]
			continue
		}

		if f.paste {
			n, done := f.collectPaste(f.buf)
			f.buf = f.buf[n:]
			if done {
				out = append(out, Event{Kind: KindPaste, Text: string(f.pasteBuf)})
				f.pasteBuf = f.pasteBuf[:0]
				f.paste = false
				continue
			}
			break
		}

		// Orphaned SGR tail with the ESC itself lost: "[<35;61;8M". Discard
		// it whole; if incomplete, wait for the terminator (or drop at flush).
		if n, ok := orphanMouseLen(f.buf); n > 0 {
			if !ok {
				if !forced {
					break // wait for 'M'/'m'
				}
				n = len(f.buf)
			}
			f.buf = f.buf[n:]
			continue
		}

		n, complete := seqLen(f.buf)
		if !complete {
			if !forced || incompleteRune(f.buf) {
				break
			}
			// At timeout, decode what we have; unknowns are dropped.
			n = len(f.buf)
		}
		raw := f.buf[:n]
		n2, ev := f.dec.Decode(f.buf)
		if n2 <= 0 || n2 > n {
			n2 = n
		}
		f.buf = f.buf[n2:]
		out = append(out, f.convert(raw[:n2], ev))
	}
	return out
}

// collectPaste consumes bytes until the bracketed-paste terminator CSI 201~.
func (f *Framer) collectPaste(b []byte) (consumed int, done bool) {
	previous := len(f.pasteBuf)
	f.pasteBuf = append(f.pasteBuf, b...)
	start := max(0, previous-len("\x1b[201~")+1)
	i := bytes.Index(f.pasteBuf[start:], []byte("\x1b[201~"))
	if i < 0 {
		return len(b), false
	}
	i += start
	f.pasteBuf = f.pasteBuf[:i]
	return i + len("\x1b[201~") - previous, true
}

func incompleteRune(b []byte) bool {
	if len(b) > 1 && b[0] == 0x1b {
		b = b[1:]
	}
	return len(b) > 0 && b[0] >= 0xc0 && !utf8.FullRune(b)
}

// dropTail removes orphaned SGR-mouse tail bytes.
func (f *Framer) dropTail(b []byte) []byte {
	if !f.tail {
		return b
	}
	for len(b) > 0 {
		c := b[0]
		switch {
		case c == 'M' || c == 'm':
			f.tail = false
			f.tailLen = 0
			b = b[1:]
		case (c >= '0' && c <= '9') || c == ';' || c == '<' || c == '[':
			f.tailLen++
			b = b[1:]
			if f.tailLen > sgrTailCap {
				f.tail = false
				f.tailLen = 0
				return b
			}
		default:
			f.tail = false
			f.tailLen = 0
			return b
		}
	}
	return b
}

// orphanMouseLen matches an SGR mouse tail whose ESC was lost entirely:
// "[<" + digits/';' + 'M'/'m'. It returns the byte count to drop and whether
// the terminator was seen. Returns (0,false) for anything else, and once the
// run exceeds sgrTailCap bytes it stops being a mouse tail.
func orphanMouseLen(b []byte) (int, bool) {
	if len(b) < 2 || b[0] != '[' || b[1] != '<' {
		return 0, false
	}
	for i := 2; i < len(b) && i <= 2+sgrTailCap; i++ {
		c := b[i]
		if c == 'M' || c == 'm' {
			return i + 1, true
		}
		if (c < '0' || c > '9') && c != ';' {
			return 0, false
		}
	}
	if len(b) >= 2+sgrTailCap {
		return 0, false
	}
	return len(b), false
}

// isIncompleteSGRMouse reports whether buf looks like a truncated SGR mouse
// report: ESC [ < followed only by digits and ';'.
func isIncompleteSGRMouse(buf []byte) bool {
	i := 0
	if len(buf) > 0 && buf[0] == 0x9b { // C1 CSI
		i = 1
	} else if len(buf) >= 2 && buf[0] == 0x1b && buf[1] == '[' {
		i = 2
	} else {
		return false
	}
	if i >= len(buf) || buf[i] != '<' {
		return false
	}
	for i++; i < len(buf); i++ {
		c := buf[i]
		if c == 'M' || c == 'm' {
			return false // complete
		}
		if (c < '0' || c > '9') && c != ';' {
			return false
		}
	}
	return true
}

// seqLen returns how many bytes the first input sequence occupies and
// whether it is complete. Text (UTF-8 runs) counts as one rune-grapheme.
func seqLen(b []byte) (int, bool) {
	if len(b) == 0 {
		return 0, false
	}
	switch b[0] {
	case 0x1b: // ESC
		if len(b) == 1 {
			return 0, false // could be a lone ESC or sequence prefix
		}
		return escSeqLen(b, 1)
	case 0x9b: // C1 CSI
		return csiLen(b, 1)
	case 0x9d: // C1 OSC
		return strLen(b, 1)
	case 0x90, 0x98, 0x9e, 0x9f: // DCS SOS PM APC
		return strLen(b, 1)
	case 0x8f: // C1 SS3
		return ss3Len(b, 1)
	default:
		if b[0] < 0x80 {
			return 1, true // ASCII control or printable
		}
		if b[0] >= 0x80 && b[0] <= 0x9f {
			return 1, true // stray C1
		}
		// UTF-8 multi-byte
		if !utf8.FullRune(b) && b[0] >= 0xc0 {
			return 0, false // incomplete utf8 sequence
		}
		_, size := utf8.DecodeRune(b)
		return size, true
	}
}

// escSeqLen measures an ESC-introduced sequence. i points past the ESC.
func escSeqLen(b []byte, i int) (int, bool) {
	if i >= len(b) {
		return 0, false
	}
	switch b[i] {
	case '[':
		return csiLen(b, i+1)
	case ']':
		return strLen(b, i+1)
	case 'P', '_', '^', 'X':
		return strLen(b, i+1)
	case 'O':
		return ss3Len(b, i+1)
	default:
		if !utf8.FullRune(b[i:]) {
			return 0, false
		}
		_, size := utf8.DecodeRune(b[i:])
		return i + size, true
	}
}

// csiLen scans a CSI body (params/intermediates/final) starting at i.
func csiLen(b []byte, i int) (int, bool) {
	for ; i < len(b); i++ {
		c := b[i]
		if c >= 0x40 && c <= 0x7e {
			return i + 1, true
		}
		if c < 0x20 || c > 0x3f {
			// Not a CSI byte; sequence is malformed — emit what we saw.
			return i, true
		}
	}
	return 0, false
}

// strLen scans a string sequence (OSC/DCS/APC/PM/SOS) ending at BEL or ESC ST.
func strLen(b []byte, i int) (int, bool) {
	for ; i < len(b); i++ {
		c := b[i]
		if c == 0x07 {
			return i + 1, true
		}
		if c == 0x1b {
			if i+1 < len(b) {
				if b[i+1] == '\\' {
					return i + 2, true
				}
				return i + 1, true // aborted by a new escape
			}
			return 0, false
		}
	}
	return 0, false
}

// ss3Len scans an SS3 sequence: optional params then a final byte.
func ss3Len(b []byte, i int) (int, bool) {
	for ; i < len(b); i++ {
		c := b[i]
		if c >= 0x40 && c <= 0x7e {
			return i + 1, true
		}
		if c < 0x30 || c > 0x3f {
			return i, true
		}
	}
	return 0, false
}

func (f *Framer) convert(raw []byte, ev uv.Event) Event {
	switch e := ev.(type) {
	case uv.CellSizeEvent, uv.PixelSizeEvent, uv.ModeReportEvent, uv.BackgroundColorEvent, uv.DarkColorSchemeEvent, uv.LightColorSchemeEvent:
		return Event{Kind: KindTerminalReport, Raw: append([]byte(nil), raw...), Report: ev}
	case uv.KeyPressEvent:
		return Event{Kind: KindKey, Raw: append([]byte(nil), raw...), Key: uv.Key(e)}
	case uv.KeyReleaseEvent:
		return Event{Kind: KindUnknown, Raw: append([]byte(nil), raw...), Key: uv.Key(e)}
	case uv.MouseClickEvent:
		return Event{Kind: KindMouseClick, Raw: append([]byte(nil), raw...), Mouse: uv.Mouse(e)}
	case uv.MouseReleaseEvent:
		return Event{Kind: KindMouseRelease, Raw: append([]byte(nil), raw...), Mouse: uv.Mouse(e)}
	case uv.MouseWheelEvent:
		return Event{Kind: KindMouseWheel, Raw: append([]byte(nil), raw...), Mouse: uv.Mouse(e)}
	case uv.MouseMotionEvent:
		return Event{Kind: KindMouseMotion, Raw: append([]byte(nil), raw...), Mouse: uv.Mouse(e)}
	case uv.PasteStartEvent:
		f.paste = true
		f.pasteBuf = f.pasteBuf[:0]
		return Event{Kind: KindUnknown, Raw: append([]byte(nil), raw...)}
	case uv.PasteEndEvent:
		// Stray end marker without a start; ignore.
		return Event{Kind: KindUnknown, Raw: append([]byte(nil), raw...)}
	case uv.FocusEvent:
		return Event{Kind: KindFocus, Raw: append([]byte(nil), raw...)}
	case uv.BlurEvent:
		return Event{Kind: KindBlur, Raw: append([]byte(nil), raw...)}
	default:
		return Event{Kind: KindUnknown, Raw: append([]byte(nil), raw...)}
	}
}
