package render

import (
	"bytes"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
)

type encodedColorKey struct {
	r, g, b, a uint32
	present    bool
}

type encodedStyleKey struct {
	colors    [3]encodedColorKey
	attrs     uint8
	underline uv.Underline
}

type encodedStyleGroup struct{ first, last int }
type encodedStylePoint struct{ position, next int }

// Match Style.Equal, including custom color implementations and default colors.
func encodedStyle(style uv.Style) encodedStyleKey {
	key := encodedStyleKey{attrs: style.Attrs, underline: style.Underline}
	for i, c := range [3]color.Color{style.Fg, style.Bg, style.UnderlineColor} {
		if c != nil {
			r, g, b, a := c.RGBA()
			key.colors[i] = encodedColorKey{r, g, b, a, true}
		}
	}
	return key
}

func (e *Encoder) beginStyleGroups() {
	e.styleGroups = e.styleGroups[:0]
	e.stylePoints = e.stylePoints[:0]
	if e.styleIndexes == nil {
		e.styleIndexes = make(map[encodedStyleKey]int)
	}
	clear(e.styleIndexes)
}

func (e *Encoder) groupStyleCell(f *Frame, x, y int) {
	key := encodedStyle(f.Cell(x, y).Style)
	index, ok := e.styleIndexes[key]
	if !ok {
		index = len(e.styleGroups)
		e.styleIndexes[key] = index
		e.styleGroups = append(e.styleGroups, encodedStyleGroup{first: -1, last: -1})
	}
	group := &e.styleGroups[index]
	next := len(e.stylePoints)
	if group.last < 0 {
		group.first = next
	} else {
		e.stylePoints[group.last].next = next
	}
	group.last = next
	e.stylePoints = append(e.stylePoints, encodedStylePoint{position: y*f.W + x, next: -1})
}

// Cursor placement makes cells independent inside the synchronized update.
// Group styles without repainting unchanged cells or splitting glyphs.
func (e *Encoder) emitStyleGroups(f *Frame, b *bytes.Buffer) {
	for _, group := range e.styleGroups {
		previousX, previousY := -1, -1
		for index := group.first; index >= 0; index = e.stylePoints[index].next {
			point := e.stylePoints[index]
			x, y := point.position%f.W, point.position/f.W
			if y != previousY || x != previousX {
				b.WriteString(cup(y, x))
			}
			cell := &f.Cells[point.position]
			e.emitCell(b, cell)
			previousX, previousY = x+max(cell.Width, 1), y
		}
	}
}
