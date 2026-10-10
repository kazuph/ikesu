package vt

import (
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// printableASCII reports whether r is a character that can never join the
// grapheme cluster beside it. No ASCII character is a combining mark, a ZWJ, a
// spacing mark or a regional indicator, and CR and LF are not printable, so a
// cluster boundary between two of these is certain without asking the
// segmenter.
func printableASCII(r rune) bool {
	return r >= ansi.SP && r < ansi.DEL
}

// asciiContent interns the single-byte strings so that batching an ASCII run
// into cells does not allocate one string per cell.
var asciiContent [128]string

func init() {
	for i := range asciiContent {
		asciiContent[i] = string(rune(i))
	}
}

// printRun writes a run of printable ASCII bytes to the screen, batching the
// per-character work of [Emulator.handleGrapheme]: cursor updates and touch
// bookkeeping happen once per row segment instead of once per byte. The last
// byte is held back in the grapheme buffer, as [Emulator.handlePrint] would
// leave it, so a combining mark following the run can still join it.
//
// It is only called when [Emulator.canFastPrint] holds: the parser is in
// ground state, no single-shift is pending, the GL charset is the default,
// the grapheme buffer is empty or holds a single ASCII byte, and the scroll
// region spans the buffer's full width.
func (e *Emulator) printRun(run []byte) {
	if len(e.grapheme) == 1 {
		// A single pending ASCII byte precedes this run.
		b := e.grapheme[0]
		e.grapheme = e.grapheme[:0]
		e.handleGrapheme(asciiContent[b], 1)
	}

	// Keep the last byte out of the batch so it stays pending in the
	// grapheme buffer exactly as handlePrint would leave it.
	pending := run[len(run)-1]
	run = run[:len(run)-1]

	awm := e.isModeSet(ansi.ModeAutoWrap)
	pen := e.scr.cursorPen()
	link := e.scr.cursorLink()
	width := e.scr.buf.Width()

	fallback := func(i int) {
		// Finish the run through the regular per-character path, in order,
		// ending with the held-back byte so it lands in the grapheme buffer.
		for ; i < len(run); i++ {
			e.handlePrint(rune(run[i]))
		}
		e.handlePrint(rune(pending))
	}

	for i := 0; i < len(run); {
		x, y := e.scr.CursorPosition()
		if e.atPhantom {
			// Resolve a pending wrap the same way handleGrapheme does.
			if awm {
				e.index()
				_, y = e.scr.CursorPosition()
				x = 0
			}
			e.atPhantom = false
		}
		line := e.scr.buf.Line(y)
		n := min(len(run)-i, width-x)

		// A direct cell assignment is only safe when every target cell is an
		// ordinary 1-wide cell; overwriting part of a wide cell needs
		// [Line.Set]'s fixups, so the rest goes through the regular path.
		fast := line != nil
		for k := 0; fast && k < n; k++ {
			if line[x+k].Width != 1 {
				fast = false
			}
		}
		if !fast {
			fallback(i)
			return
		}

		first, last := n, 0
		for k := 0; k < n; k++ {
			c := uv.Cell{Content: asciiContent[run[i+k]], Width: 1, Style: pen, Link: link}
			if line[x+k] != c {
				if k < first {
					first = k
				}
				last = k + 1
			}
			line[x+k] = c
		}
		if first < last {
			e.scr.buf.TouchLine(x+first, y, last-first)
		}
		i += n
		x += n

		if x >= width {
			if awm {
				// The next printed character wraps to the next line.
				e.atPhantom = true
			} else if i < len(run) {
				// Without autowrap the cursor stays on the last cell and
				// every remaining byte overwrites it; the last one wins.
				c := uv.Cell{Content: asciiContent[run[len(run)-1]], Width: 1, Style: pen, Link: link}
				if line[x-1] != c {
					e.scr.buf.TouchLine(x-1, y, 1)
				}
				line[x-1] = c
				i = len(run)
			}
			x = width - 1
		}
		e.scr.setCursor(x, y, false)
	}

	e.grapheme = append(e.grapheme[:0], pending)
}

// handlePrint handles printable characters.
//
// A character is held back rather than printed straight away, because the one
// after it may be a combining mark that belongs to the same cluster. Printing
// "e" from "e\u0301" before the mark arrives puts the base in one cell and
// leaves the mark to become a zero-width cell of its own, which the next write
// or erase then destroys.
func (e *Emulator) handlePrint(r rune) {
	// No ASCII character can extend the cluster before it: none of them is
	// Extend, ZWJ, SpacingMark, Prepend, a regional indicator, or part of an
	// Indic conjunct. So an ASCII character arriving is proof that whatever is
	// buffered is finished, and the buffer can go out.
	//
	// This is also what keeps the buffer from being re-segmented as it grows.
	// Asking where the clusters are on every character costs the length of the
	// buffer each time, which is quadratic over a long run of combining marks
	// that never resolves into more than one cluster.
	if printableASCII(r) {
		// Two printable ASCII characters in a row is the common case by a wide
		// margin, and needs no segmenting at all.
		if len(e.grapheme) == 1 && printableASCII(rune(e.grapheme[0])) {
			e.handleGrapheme(string(e.grapheme[0]), 1)
			e.grapheme = utf8.AppendRune(e.grapheme[:0], r)
			return
		}
		e.flushGrapheme()
	}

	e.grapheme = utf8.AppendRune(e.grapheme, r)
}

// flushGrapheme flushes the current grapheme buffer, if any, and handles the
// grapheme as a single unit.
func (e *Emulator) flushGrapheme() {
	if len(e.grapheme) == 0 {
		return
	}

	// XXX: We always use [ansi.GraphemeWidth] here to report accurate widths
	// and it's up to the caller to decide how to handle Unicode vs non-Unicode
	// modes.
	method := ansi.GraphemeWidth
	graphemes := e.grapheme
	for len(graphemes) > 0 {
		cluster, width := ansi.FirstGraphemeCluster(graphemes, method)
		if len(cluster) == 0 {
			break
		}
		e.handleGrapheme(string(cluster), width)
		graphemes = graphemes[len(cluster):]
	}
	e.grapheme = e.grapheme[:0] // Reset the grapheme buffer.
}

// handleGrapheme handles UTF-8 graphemes.
func (e *Emulator) handleGrapheme(content string, width int) {
	awm := e.isModeSet(ansi.ModeAutoWrap)
	cell := uv.Cell{
		Content: content,
		Width:   width,
		Style:   e.scr.cursorPen(),
		Link:    e.scr.cursorLink(),
	}

	x, y := e.scr.CursorPosition()
	if awm && (e.atPhantom || x+width > e.scr.Width()) {
		// moves cursor down similar to [Terminal.linefeed] except it doesn't
		// respects [ansi.LNM] mode.
		// This will reset the phantom state i.e. pending wrap state.
		e.index()
		_, y = e.scr.CursorPosition()
		x = 0
	}

	// A single shift applies to the next character, whatever that character
	// turns out to be. Take it now, so it cannot leak onto the one after this
	// when this is a cluster no charset has a mapping for.
	single := e.gsingle
	e.gsingle = 0

	// Handle character set mappings
	if len(content) == 1 { //nolint:nestif
		var charset CharSet
		c := content[0]
		if single > 1 && single < 4 {
			charset = e.charsets[single]
		} else if c < 128 {
			charset = e.charsets[e.gl]
		} else {
			charset = e.charsets[e.gr]
		}

		if charset != nil {
			if r, ok := charset[c]; ok {
				cell.Content = r
				cell.Width = 1
			}
		}
	}

	if cell.Width == 1 && len(content) == 1 {
		e.lastChar, _ = utf8.DecodeRuneInString(content)
	}

	e.scr.SetCell(x, y, &cell)

	// Handle phantom state at the end of the line
	e.atPhantom = awm && x+cell.Width >= e.scr.Width()
	if e.atPhantom {
		x = e.scr.Width() - 1
	} else {
		x += cell.Width
	}

	// NOTE: We don't reset the phantom state here, we handle it up above.
	e.scr.setCursor(x, y, false)
}
