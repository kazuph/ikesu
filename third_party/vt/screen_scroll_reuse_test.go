package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"image/color"
	"reflect"
	"slices"
	"testing"
)

func (s *Scrollback) originalPush(line uv.Line) {
	if s == nil || s.maxLines <= 0 {
		return
	}

	// Find last non-empty cell to trim trailing empty cells.
	// This helps with wrapping and window resizing.
	lastNonEmpty := -1
	for i := len(line) - 1; i >= 0; i-- {
		if !blankCell(&line[i]) {
			lastNonEmpty = i
			break
		}
	}

	// Keep the line content up to and including the last non-empty cell.
	end := lastNonEmpty + 1
	if lastNonEmpty >= 0 {
		// Keep a wide glyph's continuation cells so a later blank cannot
		// overwrite its head when the saved row is rendered again.
		end = min(lastNonEmpty+max(line[lastNonEmpty].Width, 1), len(line))
	}

	if s.byteLimited {
		s.pushBytes(line[:end])
		return
	}
	if s.count < s.maxLines {
		s.lines[s.wrap(s.start+s.count)] = slices.Clone(line[:end])
		s.count++
		return
	}

	// The buffer is full: reuse the evicted line's backing array for the new
	// copy instead of allocating a fresh one for every pushed line.
	idx := s.start
	if dst := s.lines[idx]; cap(dst) >= end {
		s.lines[idx] = append(dst[:0], line[:end]...)
	} else {
		s.lines[idx] = slices.Clone(line[:end])
	}
	s.start = s.wrap(s.start + 1)
}

func (s *Screen) originalDeleteLine(n int) bool {
	if n <= 0 {
		return false
	}

	scroll := s.scroll
	x, y := s.cur.X, s.cur.Y

	// Only operate if cursor Y is within scroll region
	if y < scroll.Min.Y || y >= scroll.Max.Y ||
		x < scroll.Min.X || x >= scroll.Max.X {
		return false
	}

	// Save lines to scrollback if we're at the top of the scroll region
	// and the scroll region uses the full width (typical terminal scroll).
	// This captures lines that would be lost during scroll up operations.
	if s.scrollback != nil && y == scroll.Min.Y &&
		scroll.Min.X == 0 && scroll.Max.X == s.buf.Width() {
		// Save lines that will be deleted
		linesToSave := min(n, scroll.Max.Y-y)
		for row := y; row < y+linesToSave; row++ {
			if l := s.buf.Line(row); l != nil {
				s.scrollback.originalPush(l)
			}
		}
	}

	s.buf.DeleteLineArea(y, n, s.blankCell(), scroll)
	if s.cb.Scroll != nil {
		s.cb.Scroll(scroll.Min.X, y, scroll.Max.X, scroll.Max.Y, -min(n, scroll.Max.Y-y))
	}

	return true
}

func TestScrollReuseMatchesOriginal(t *testing.T) {
	cases := 0
	cells := []uv.Cell{uv.EmptyCell, {}, {Content: "x", Width: 1}, {Content: "世", Width: 2}, {Content: " ", Width: 1, Style: uv.Style{Attrs: uv.AttrBold}}, {Content: " ", Width: 1, Link: uv.Link{URL: "x"}}, {Content: " ", Width: 1, Style: uv.Style{Bg: color.RGBA{R: 1, A: 255}}}}
	for _, width := range []int{1, 3, 8, 160} {
		for _, height := range []int{1, 5} {
			for _, kind := range []string{"lines", "bytes", "nil"} {
				for _, pattern := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11} {
					for _, bg := range []bool{false, true} {
						for _, n := range []int{-1, 0, 1, 2, 8} {
							for _, top := range []int{0, height / 2} {
								for _, bottom := range []int{height, max(top+1, height-1)} {
									var ga, wa [][5]int
									makeScreen := func(events *[][5]int) *Screen {
										s := NewScreen(width, height)
										s.cb = &Callbacks{Scroll: func(a, b, c, d, e int) { *events = append(*events, [5]int{a, b, c, d, e}) }}
										s.scroll = uv.Rect(0, top, width, bottom-top)
										s.cur.Y = top
										if bg {
											s.cur.Pen.Bg = color.RGBA{G: 1, A: 255}
										}
										switch kind {
										case "lines":
											s.scrollback = NewScrollback(2)
										case "bytes":
											s.scrollback = NewScrollback(2)
											s.scrollback.SetMaxBytes(10000)
										case "nil":
											s.scrollback = nil
										}
										for y := 0; y < height; y++ {
											for x := 0; x < width; x++ {
												if pattern < 7 && x < width/2 {
													s.buf.Lines[y][x] = cells[(x+y+pattern)%len(cells)]
												} else if pattern == 7 {
													s.buf.Lines[y][x] = cells[(x+y)%2]
												}
											}
										}
										if pattern == 8 {
											for y := range s.buf.Lines {
												for x := range s.buf.Lines[y] {
													s.buf.Lines[y][x] = uv.Cell{}
												}
											}
										}
										if pattern == 9 {
											s.buf.Lines[top] = nil
										}
										if pattern == 10 {
											s.buf.Lines[0] = nil
										}
										if pattern == 11 {
											for y := range s.buf.Lines {
												s.buf.Lines[y][width-1] = cells[6]
											}
										}
										return s
									}
									got, want := makeScreen(&ga), makeScreen(&wa)
									for rep := 0; rep < 4; rep++ {
										gr, wr := got.DeleteLine(n), want.originalDeleteLine(n)
										if gr != wr || !reflect.DeepEqual(got.buf, want.buf) || !reflect.DeepEqual(ga, wa) {
											t.Fatalf("screen w=%d h=%d kind=%s pattern=%d bg=%v n=%d top=%d bottom=%d rep=%d", width, height, kind, pattern, bg, n, top, bottom, rep)
										}
										if got.scrollback != nil {
											if !reflect.DeepEqual(got.scrollback.Lines(), want.scrollback.Lines()) || got.scrollback.Bytes() != want.scrollback.Bytes() {
												t.Fatalf("history w=%d h=%d pattern=%d rep=%d", width, height, pattern, rep)
											}
										}
										cases++
									}
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("screen, dirty ranges, callbacks and retained rows matched in %d cases", cases)
}
