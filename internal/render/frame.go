// Package render turns application state and pane snapshots into a frame of
// styled cells, and diffs frames into terminal byte output. It never touches
// PTYs, files, or OS APIs.
package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Frame is a grid of styled cells covering the whole screen.
type Frame struct {
	W, H  int
	Cells []uv.Cell
}

// NewFrame allocates an empty frame.
func NewFrame(w, h int) *Frame {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	cells := make([]uv.Cell, w*h)
	for i := range cells {
		cells[i] = uv.Cell{Content: " ", Width: 1}
	}
	return &Frame{W: w, H: h, Cells: cells}
}

// Cell returns the cell at (x, y) or nil when out of bounds.
func (f *Frame) Cell(x, y int) *uv.Cell {
	if x < 0 || y < 0 || x >= f.W || y >= f.H {
		return nil
	}
	return &f.Cells[y*f.W+x]
}

// Set writes one cell. Wide content marks the following cell as a
// placeholder (width 0); overwriting either half of an existing wide cell
// clears the other half.
func (f *Frame) Set(x, y int, content string, style uv.Style) {
	c := f.Cell(x, y)
	if c == nil {
		return
	}
	// Overwriting the second half of a wide glyph: clear its head.
	if c.Width == 0 {
		if prev := f.Cell(x-1, y); prev != nil && prev.Width == 2 {
			*prev = uv.Cell{Content: " ", Width: 1}
		}
	}
	// Overwriting a wide head: clear its tail so it doesn't stay stale.
	if c.Width == 2 {
		if n := f.Cell(x+1, y); n != nil && n.Width == 0 {
			*n = uv.Cell{Content: " ", Width: 1}
		}
	}
	w := ansi.StringWidth(content)
	if w < 1 {
		w = 1
	}
	*c = uv.Cell{Content: content, Style: style, Width: w}
	if w == 2 {
		if n := f.Cell(x+1, y); n != nil {
			*n = uv.Cell{Width: 0}
		}
	}
}

// Text draws a string starting at (x, y); returns the next x. Stops at the
// frame edge and at stopX (exclusive) when stopX > x.
func (f *Frame) Text(x, y int, s string, style uv.Style, stopX ...int) int {
	limit := f.W
	if len(stopX) > 0 && stopX[0] < limit {
		limit = stopX[0]
	}
	for _, r := range s {
		w := ansi.StringWidth(string(r))
		if w < 1 {
			w = 1
		}
		if x+w > limit {
			break
		}
		f.Set(x, y, string(r), style)
		x += w
	}
	return x
}

// Fill paints every cell of rect with content+style.
func (f *Frame) Fill(x, y, w, h int, content string, style uv.Style) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			f.Set(xx, yy, content, style)
		}
	}
}

// Box draws a single-line border around rect and returns the interior rect
// (x+1, y+1, w-2, h-2).
func (f *Frame) Box(x, y, w, h int, style uv.Style) (ix, iy, iw, ih int) {
	if w < 2 || h < 2 {
		return x, y, w, h
	}
	for xx := x; xx < x+w; xx++ {
		f.Set(xx, y, "─", style)
		f.Set(xx, y+h-1, "─", style)
	}
	for yy := y; yy < y+h; yy++ {
		f.Set(x, yy, "│", style)
		f.Set(x+w-1, yy, "│", style)
	}
	f.Set(x, y, "┌", style)
	f.Set(x+w-1, y, "┐", style)
	f.Set(x, y+h-1, "└", style)
	f.Set(x+w-1, y+h-1, "┘", style)
	return x + 1, y + 1, w - 2, h - 2
}

// CopyGrid copies a grid of cells ([row][col]) into the frame at (x, y),
// clipping to the frame and the grid.
func (f *Frame) CopyGrid(x, y int, grid [][]uv.Cell) {
	for gy, row := range grid {
		if y+gy < 0 || y+gy >= f.H {
			continue
		}
		for gx, c := range row {
			if x+gx < 0 || x+gx >= f.W {
				continue
			}
			if c.Width == 0 {
				continue
			}
			f.Set(x+gx, y+gy, c.Content, c.Style)
		}
	}
}
