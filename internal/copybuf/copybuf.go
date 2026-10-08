// Package copybuf implements the retained-buffer motions and search of
// the pane copy contract (SPEC G6 / pane.copy_motion, pane.copy_search):
// vi-style word/big-word motions, line ends, paragraphs, case-sensitive
// non-overlapping search, and the wrapping "current match" index.
package copybuf

import (
	"strings"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
)

// Point is an absolute buffer coordinate (scrollback rows first).
type Point struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// Range is an inclusive match span in buffer coordinates.
type Range struct {
	Start Point `json:"start"`
	End   Point `json:"end"`
}

// Motion names accepted by pane.copy_motion.
const (
	MotionLineEnd              = "line_end"
	MotionFirstNonBlank        = "first_non_blank"
	MotionNextWordStart        = "next_word_start"
	MotionPreviousWordStart    = "previous_word_start"
	MotionNextWordEnd          = "next_word_end"
	MotionNextBigWordStart     = "next_big_word_start"
	MotionPreviousBigWordStart = "previous_big_word_start"
	MotionNextBigWordEnd       = "next_big_word_end"
	MotionPreviousParagraph    = "previous_paragraph"
	MotionNextParagraph        = "next_paragraph"
)

// Motions lists every accepted motion (used for error messages).
var Motions = []string{
	MotionLineEnd, MotionFirstNonBlank,
	MotionNextWordStart, MotionPreviousWordStart, MotionNextWordEnd,
	MotionNextBigWordStart, MotionPreviousBigWordStart, MotionNextBigWordEnd,
	MotionPreviousParagraph, MotionNextParagraph,
}

// cellRune returns the rune occupying cell x, or 0 for blanks/spacers.
func cellRune(row []uv.Cell, x int) rune {
	if x < 0 || x >= len(row) {
		return 0
	}
	r := []rune(strings.TrimSpace(row[x].Content))
	if len(r) == 0 {
		return 0
	}
	return r[0]
}

const (
	clsBlank = iota
	clsWord  // [alnum_] or, for big words, any non-blank
	clsPunct // other non-blank
)

// classOf classifies the cell at x for word (big=false) or WORD
// (big=true) motions.
func classOf(row []uv.Cell, x int, big bool) int {
	r := cellRune(row, x)
	if r == 0 || unicode.IsSpace(r) {
		return clsBlank
	}
	if big {
		return clsWord
	}
	if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
		return clsWord
	}
	return clsPunct
}

// LineLen is the real text length of a row (trailing blanks trimmed); a
// cursor stops at its last char (SPEC G6 行末停止).
func LineLen(row []uv.Cell) int {
	last := -1
	for x := range row {
		if cellRune(row, x) != 0 {
			last = x + max(row[x].Width, 1) - 1
		}
	}
	return last + 1
}

// isWordStart reports whether (y,x) begins a word run: non-blank and
// either at column 0 / after a blank / after a different class.
func isWordStart(lines [][]uv.Cell, y, x int, big bool) bool {
	row := lines[y]
	c := classOf(row, x, big)
	return c != clsBlank && classOf(row, x-1, big) != c
}

// isWordEnd reports whether (y,x) ends a word run.
func isWordEnd(lines [][]uv.Cell, y, x int, big bool) bool {
	row := lines[y]
	c := classOf(row, x, big)
	return c != clsBlank && classOf(row, x+1, big) != c
}

// isBlankRow reports whether a row has no non-blank cells.
func isBlankRow(row []uv.Cell) bool { return LineLen(row) == 0 }

// scanStart scans forward (dir=1) or backward (dir=-1) for the first
// word start strictly after the cursor position.
func scanStart(lines [][]uv.Cell, from Point, dir int, big bool) (Point, bool) {
	n := len(lines)
	x, y := from.Col+dir, from.Row
	for y >= 0 && y < n {
		row := lines[y]
		for x >= 0 && x < len(row) {
			if isWordStart(lines, y, x, big) {
				return Point{y, x}, true
			}
			x += dir
		}
		y += dir
		if dir > 0 {
			x = 0
		} else if y >= 0 && y < n {
			x = len(lines[y]) - 1
		}
	}
	return from, false
}

// scanEnd scans forward for the first word end strictly after the cursor.
func scanEnd(lines [][]uv.Cell, from Point, big bool) (Point, bool) {
	n := len(lines)
	x, y := from.Col+1, from.Row
	for y < n {
		row := lines[y]
		for x < len(row) {
			if isWordEnd(lines, y, x, big) {
				return Point{y, x}, true
			}
			x++
		}
		y++
		x = 0
	}
	return from, false
}

// Motion applies the named copy motion to cursor (row,col) over the
// retained buffer. ok=false for an unknown motion.
func Motion(lines [][]uv.Cell, motion string, row, col int) (Point, bool) {
	n := len(lines)
	if n == 0 || row < 0 || row >= n {
		return Point{row, col}, false
	}
	from := Point{row, col}
	cur := lines[row]
	switch motion {
	case MotionLineEnd:
		return Point{row, max(LineLen(cur)-1, 0)}, true
	case MotionFirstNonBlank:
		for x := 0; x < len(cur); x++ {
			if cellRune(cur, x) != 0 {
				return Point{row, x}, true
			}
		}
		return Point{row, 0}, true
	case MotionNextWordStart, MotionNextBigWordStart:
		p, _ := scanStart(lines, from, 1, motion == MotionNextBigWordStart)
		return p, true
	case MotionPreviousWordStart, MotionPreviousBigWordStart:
		p, _ := scanStart(lines, from, -1, motion == MotionPreviousBigWordStart)
		return p, true
	case MotionNextWordEnd, MotionNextBigWordEnd:
		p, _ := scanEnd(lines, from, motion == MotionNextBigWordEnd)
		return p, true
	case MotionPreviousParagraph:
		for y := row - 1; y >= 0; y-- {
			if isBlankRow(lines[y]) {
				return Point{y, 0}, true
			}
		}
		return Point{0, 0}, true
	case MotionNextParagraph:
		for y := row + 1; y < n; y++ {
			if isBlankRow(lines[y]) {
				return Point{y, 0}, true
			}
		}
		return Point{n - 1, 0}, true
	}
	return from, false
}

// RowText flattens a row to its text and the cell column of each rune
// (wide-char cells count once at their first column).
func RowText(row []uv.Cell) (string, []int) {
	var sb strings.Builder
	var cols []int
	for x, c := range row {
		runes := []rune(c.Content)
		for _, r := range runes {
			cols = append(cols, x)
			sb.WriteRune(r)
		}
	}
	return sb.String(), cols
}

// Search returns every non-overlapping occurrence of query in the
// buffer, case-sensitively, in buffer order (matches do not overlap:
// "aa" in "aaaa" yields two ranges).
func Search(lines [][]uv.Cell, query string) []Range {
	if query == "" {
		return nil
	}
	qr := []rune(query)
	var out []Range
	for y, row := range lines {
		text, cols := RowText(row)
		runes := []rune(text)
		for i := 0; i+len(qr) <= len(runes); {
			match := true
			for j, q := range qr {
				if runes[i+j] != q {
					match = false
					break
				}
			}
			if match {
				out = append(out, Range{
					Start: Point{y, cols[i]},
					End:   Point{y, cols[i+len(qr)-1]},
				})
				i += len(qr)
			} else {
				i++
			}
		}
	}
	return out
}

// CurrentIndex returns the index of the match the cursor would land on:
// forward = first match strictly after the anchor, backward = last match
// strictly before it, wrapping around. -1 when there are no matches.
func CurrentIndex(matches []Range, anchor Point, forward bool) int {
	if len(matches) == 0 {
		return -1
	}
	if forward {
		for i, m := range matches {
			if m.Start.Row > anchor.Row || (m.Start.Row == anchor.Row && m.Start.Col > anchor.Col) {
				return i
			}
		}
		return 0
	}
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		if m.Start.Row < anchor.Row || (m.Start.Row == anchor.Row && m.Start.Col < anchor.Col) {
			return i
		}
	}
	return len(matches) - 1
}
