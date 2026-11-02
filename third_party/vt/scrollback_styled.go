package vt

import (
	"strings"
	"unsafe"

	uv "github.com/charmbracelet/ultraviolet"
)

type savedGlyph struct {
	end   int
	width int
}
type savedStyle struct {
	style uv.Style
	link  uv.Link
	end   int
}
type savedRun struct {
	end   int
	style int
}
type styledRow struct {
	glyphs []savedGlyph
	runs   []savedRun
	styles []savedStyle
	text   string
	zero   []bool
}

// Runs retain the original grapheme/column sequence and share only equal
// appearance. Dense changes keep cells when run storage would cost more.
func packStyled(line uv.Line, reuse *styledRow) *styledRow {
	if len(line) == 0 {
		return nil
	}
	reused := reuse != nil
	if !reused {
		reuse = &styledRow{}
	}
	if cap(reuse.glyphs) < len(line) {
		reuse.glyphs = make([]savedGlyph, len(line))
		reuse.zero = make([]bool, len(line))
	} else {
		reuse.glyphs = reuse.glyphs[:len(line)]
		reuse.zero = reuse.zero[:len(line)]
	}
	oldStyles := len(reuse.styles)
	reuse.runs = reuse.runs[:0]
	reuse.styles = reuse.styles[:0]
	var text strings.Builder
	size := 0
	for i := range line {
		size += len(line[i].Content)
	}
	text.Grow(size)
	reuse.styles = append(reuse.styles, savedStyle{style: line[0].Style, link: line[0].Link})
	reuse.runs = append(reuse.runs, savedRun{})
	var styleIndex map[appearanceKey]int
	run := &reuse.runs[0]
	text.WriteString(line[0].Content)
	reuse.glyphs[0] = savedGlyph{text.Len(), line[0].Width}
	reuse.zero[0] = line[0].Width == 0 && line[0].Content == "" && blankCell(&line[0])
	for x := 1; x < len(line); x++ {
		c := &line[x]
		text.WriteString(c.Content)
		reuse.glyphs[x] = savedGlyph{text.Len(), c.Width}
		reuse.zero[x] = c.Width == 0 && c.Content == "" && blankCell(c)
		if !reuse.zero[x] && !sameSavedStyle(&reuse.styles[run.style], c) {
			run.end = x
			// Keep two common alternating appearances on the allocation-free path.
			style, found := 0, false
			for style < len(reuse.styles) && style < 2 {
				if sameSavedStyle(&reuse.styles[style], c) {
					found = true
					break
				}
				style++
			}
			if !found && styleIndex == nil && len(reuse.styles) >= 2 {
				styleIndex = make(map[appearanceKey]int)
				for i := range reuse.styles {
					styleIndex[savedAppearanceKey(&reuse.styles[i])] = i
				}
			}
			var key appearanceKey
			if !found && styleIndex != nil {
				key = cellAppearanceKey(c)
				style, found = styleIndex[key]
			}
			if !found {
				style = len(reuse.styles)
				reuse.styles = append(reuse.styles, savedStyle{style: c.Style, link: c.Link})
				if styleIndex != nil {
					styleIndex[key] = style
				}
			}
			reuse.runs = append(reuse.runs, savedRun{style: style})
			run = &reuse.runs[len(reuse.runs)-1]
		}
	}
	run.end = len(line)
	reuse.text = text.String()
	if oldStyles > len(reuse.styles) {
		clear(reuse.styles[len(reuse.styles):oldStyles])
	}
	cost := int(unsafe.Sizeof(styledRow{})) + cap(reuse.glyphs)*int(unsafe.Sizeof(savedGlyph{})) + cap(reuse.runs)*int(unsafe.Sizeof(savedRun{})) + cap(reuse.styles)*int(unsafe.Sizeof(savedStyle{})) + len(reuse.text) + cap(reuse.zero)
	if cost > len(line)*scrollbackCellBytes {
		if reused {
			// Storage kept from a wider evicted row can exceed a short row's
			// budget on its own; retry with exact-size storage.
			return packStyled(line, nil)
		}
		return nil
	}
	return reuse
}

func (r *styledRow) contentAt(x int) string {
	start := 0
	if x > 0 {
		start = r.glyphs[x-1].end
	}
	return r.text[start:r.glyphs[x].end]
}

func (r *styledRow) cellAt(x int) *uv.Cell {
	if x < 0 || x >= len(r.glyphs) {
		return nil
	}
	if r.zero[x] {
		return &uv.Cell{}
	}
	for i := range r.runs {
		if x < r.runs[i].end {
			g := r.glyphs[x]
			appearance := r.styles[r.runs[i].style]
			return &uv.Cell{Content: r.contentAt(x), Width: g.width, Style: appearance.style, Link: appearance.link}
		}
	}
	return nil
}
func (r *styledRow) line() uv.Line {
	row := make(uv.Line, len(r.glyphs))
	start := 0
	for _, run := range r.runs {
		appearance := r.styles[run.style]
		for x := start; x < run.end; x++ {
			if r.zero[x] {
				continue
			}
			g := r.glyphs[x]
			row[x] = uv.Cell{Content: r.contentAt(x), Width: g.width, Style: appearance.style, Link: appearance.link}
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

type appearanceKey [appearanceBytes / unsafe.Sizeof(uintptr(0))]uintptr

func savedAppearanceKey(r *savedStyle) appearanceKey {
	return *(*appearanceKey)(unsafe.Pointer(&r.style))
}

func cellAppearanceKey(c *uv.Cell) appearanceKey {
	return *(*appearanceKey)(unsafe.Pointer(&c.Style))
}

func sameSavedStyle(r *savedStyle, c *uv.Cell) bool {
	a := (*[appearanceBytes / unsafe.Sizeof(uintptr(0))]uintptr)(unsafe.Pointer(&r.style))
	b := (*[appearanceBytes / unsafe.Sizeof(uintptr(0))]uintptr)(unsafe.Pointer(&c.Style))
	return a[0] == b[0] && a[1] == b[1] && a[2] == b[2] && a[3] == b[3] && a[4] == b[4] && a[5] == b[5] && a[6] == b[6] && a[7] == b[7] && a[8] == b[8] && a[9] == b[9] && a[10] == b[10]
}
