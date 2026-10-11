package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"unsafe"
)

type savedGlyph struct {
	content string
	width   int
}
type savedStyle struct {
	style uv.Style
	link  uv.Link
	end   int
}
type styledRow struct {
	glyphs []savedGlyph
	runs   []savedStyle
}

// Runs retain the original grapheme/column sequence and share only equal
// appearance. Dense changes keep cells when run storage would cost more.
func packStyled(line uv.Line, reuse *styledRow) *styledRow {
	if len(line) == 0 {
		return nil
	}
	if reuse == nil {
		reuse = &styledRow{}
	}
	if cap(reuse.glyphs) != len(line) {
		reuse.glyphs = make([]savedGlyph, len(line))
	} else {
		reuse.glyphs = reuse.glyphs[:len(line)]
	}
	oldRuns := len(reuse.runs)
	reuse.runs = reuse.runs[:0]
	reuse.runs = append(reuse.runs, savedStyle{style: line[0].Style, link: line[0].Link})
	run := &reuse.runs[0]
	reuse.glyphs[0] = savedGlyph{line[0].Content, line[0].Width}
	for x := 1; x < len(line); x++ {
		c := &line[x]
		reuse.glyphs[x] = savedGlyph{c.Content, c.Width}
		if !sameSavedStyle(run, c) {
			run.end = x
			reuse.runs = append(reuse.runs, savedStyle{style: c.Style, link: c.Link})
			run = &reuse.runs[len(reuse.runs)-1]
		}
	}
	run.end = len(line)
	if oldRuns > len(reuse.runs) {
		clear(reuse.runs[len(reuse.runs):oldRuns])
	}
	cost := int(unsafe.Sizeof(styledRow{})) + cap(reuse.glyphs)*int(unsafe.Sizeof(savedGlyph{})) + cap(reuse.runs)*int(unsafe.Sizeof(savedStyle{}))
	if cost > len(line)*scrollbackCellBytes {
		return nil
	}
	return reuse
}
func (r *styledRow) cellAt(x int) *uv.Cell {
	if x < 0 || x >= len(r.glyphs) {
		return nil
	}
	for i := range r.runs {
		if x < r.runs[i].end {
			g := r.glyphs[x]
			return &uv.Cell{Content: g.content, Width: g.width, Style: r.runs[i].style, Link: r.runs[i].link}
		}
	}
	return nil
}
func (r *styledRow) line() uv.Line {
	row := make(uv.Line, len(r.glyphs))
	start := 0
	for _, run := range r.runs {
		for x := start; x < run.end; x++ {
			g := r.glyphs[x]
			row[x] = uv.Cell{Content: g.content, Width: g.width, Style: run.style, Link: run.link}
		}
		start = run.end
	}
	return row
}

// These adjacent fields contain only the immutable appearance references and
// values copied from the screen. Identity equality is sufficient to share a
// run, including non-comparable color implementations; unequal identities
// simply start another lossless run. Read-only byte comparison avoids calling
// color methods for every cell. The layout is checked by the contract test.
const appearanceBytes = unsafe.Sizeof(uv.Style{}) + unsafe.Sizeof(uv.Link{})

func sameSavedStyle(r *savedStyle, c *uv.Cell) bool {
	a := (*[appearanceBytes / unsafe.Sizeof(uintptr(0))]uintptr)(unsafe.Pointer(&r.style))
	b := (*[appearanceBytes / unsafe.Sizeof(uintptr(0))]uintptr)(unsafe.Pointer(&c.Style))
	return a[0] == b[0] && a[1] == b[1] && a[2] == b[2] && a[3] == b[3] && a[4] == b[4] && a[5] == b[5] && a[6] == b[6] && a[7] == b[7] && a[8] == b[8] && a[9] == b[9] && a[10] == b[10]
}
