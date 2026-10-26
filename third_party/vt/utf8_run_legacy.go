//go:build !flatcells

package vt

import uv "github.com/charmbracelet/ultraviolet"

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

	awm := e.autoWrap
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
		plain := pen == (uv.Style{}) && link == (uv.Link{})
		fast := line != nil
		for k := 0; fast && k < n; k++ {
			c := &line[x+k]
			if plain {
				st := &c.Style
				plain = int(st.Attrs)|int(st.Underline)|len(c.Link.URL)|len(c.Link.Params) == 0 && st.Fg == nil && st.Bg == nil && st.UnderlineColor == nil
			}
			if c.Width != 1 {
				fast = false
			}
		}
		if !fast {
			fallback(i)
			return
		}

		var cells uv.Line
		var data []byte
		if n > 0 {
			cells = line[x : x+n]
			data = run[i : i+n]
			_ = cells[len(data)-1]
		}
		first, last := n, 0
		c := uv.Cell{Width: 1, Style: pen, Link: link}
		var touched *uv.LineData
		if len(e.scr.buf.Touched) >= len(e.scr.buf.Lines) {
			touched = e.scr.buf.Touched[y]
		}
		if touched != nil && touched.FirstCell <= x && touched.LastCell >= x+n && plain {
			for k, b := range data {
				cells[k].Content = asciiContent[b]
			}
		} else if touched != nil && touched.FirstCell <= x && touched.LastCell >= x+n {
			for k, b := range data {
				c.Content = asciiContent[b]
				cells[k] = c
			}
		} else {
			for k, b := range data {
				c.Content = asciiContent[b]
				if cells[k] != c {
					if k < first {
						first = k
					}
					last = k + 1
				}
				cells[k] = c
			}
			if first < last {
				e.scr.buf.TouchLine(x+first, y, last-first)
			}
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
