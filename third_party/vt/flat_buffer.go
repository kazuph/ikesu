//go:build flatcells

package vt

import uv "github.com/charmbracelet/ultraviolet"

// flatScreenBuffer owns one pointer-free cell allocation and integer row order.
// Line and CellAt return decoded values; writes use explicit storage operations.
type flatScreenBuffer struct {
	cells   []FlatCell
	rows    []int
	lengths []int
	stride  int
	head    int
	tables  flatTables
	Touched []*uv.LineData
}

func newFlatScreenBuffer(width, height int) *flatScreenBuffer {
	b := &flatScreenBuffer{}
	b.Resize(width, height)
	b.Touched = make([]*uv.LineData, height)
	return b
}

func (b *flatScreenBuffer) Height() int { return len(b.rows) }

func (b *flatScreenBuffer) physical(y int) int {
	return b.rows[(b.head+y)%len(b.rows)]
}

func (b *flatScreenBuffer) rowLength(y int) int {
	if y < 0 || y >= len(b.rows) {
		return -1
	}
	return b.lengths[b.physical(y)]
}

func (b *flatScreenBuffer) Width() int           { return max(0, b.rowLength(0)) }
func (b *flatScreenBuffer) Bounds() uv.Rectangle { return uv.Rect(0, 0, b.Width(), b.Height()) }

func (b *flatScreenBuffer) raw(x, y int) FlatCell {
	return b.cells[b.physical(y)*b.stride+x]
}

func (b *flatScreenBuffer) assign(x, y int, c FlatCell) {
	i := b.physical(y)*b.stride + x
	old := b.cells[i]
	if old == c {
		return
	}
	b.tables.retain(c)
	b.tables.release(old)
	b.cells[i] = c
}

func (b *flatScreenBuffer) setRaw(x, y int, c uv.Cell) {
	packed := b.tables.pack(c)
	b.assign(x, y, packed)
	b.tables.release(packed)
}

func (b *flatScreenBuffer) CellAt(x, y int) *uv.Cell {
	if x < 0 || x >= b.rowLength(y) {
		return nil
	}
	c := b.tables.unpack(b.raw(x, y))
	return &c
}

func (b *flatScreenBuffer) Line(y int) uv.Line {
	n := b.rowLength(y)
	if n < 0 {
		return nil
	}
	line := make(uv.Line, n)
	b.copyRow(line, 0, y)
	return line
}

func (b *flatScreenBuffer) copyRow(dst []uv.Cell, x, y int) {
	n := b.rowLength(y)
	for i := range dst {
		if x+i < 0 || x+i >= n {
			dst[i] = uv.EmptyCell
		} else {
			dst[i] = b.tables.unpack(b.raw(x+i, y))
		}
	}
}

func (b *flatScreenBuffer) putLine(y int, line uv.Line) {
	if y < 0 || y >= b.Height() {
		return
	}
	for x := 0; x < min(b.rowLength(y), len(line)); x++ {
		b.setRaw(x, y, line[x])
	}
}

func (b *flatScreenBuffer) TouchLine(x, y, n int) {
	if y < 0 || y >= b.Height() {
		return
	}
	if len(b.Touched) < b.Height() {
		b.Touched = append(b.Touched, make([]*uv.LineData, b.Height()-len(b.Touched))...)
	}
	if b.Touched[y] == nil {
		b.Touched[y] = &uv.LineData{FirstCell: x, LastCell: x + n}
	} else {
		b.Touched[y].FirstCell = min(b.Touched[y].FirstCell, x)
		b.Touched[y].LastCell = max(b.Touched[y].LastCell, x+n)
	}
}

func (b *flatScreenBuffer) SetCell(x, y int, c *uv.Cell) { b.setCell(x, y, c, true) }

func (b *flatScreenBuffer) setCell(x, y int, c *uv.Cell, touch bool) {
	lineWidth := b.rowLength(y)
	if touch {
		prev := b.CellAt(x, y)
		if prev == nil || c == nil || !prev.Equal(c) {
			width := 1
			if c != nil {
				width = max(width, c.Width)
			}
			if prev != nil {
				width = max(width, prev.Width)
			}
			b.TouchLine(x, y, width)
		}
	}
	if x < 0 || x >= lineWidth {
		return
	}
	prev := b.tables.unpack(b.raw(x, y))
	if prev.Width > 1 {
		empty := prev
		empty.Empty()
		for j := 0; j < prev.Width && x+j < lineWidth; j++ {
			b.setRaw(x+j, y, empty)
		}
	} else if prev.Width == 0 {
		for j := 1; x-j >= 0; j++ {
			wide := b.tables.unpack(b.raw(x-j, y))
			if wide.Width > 1 && j < wide.Width {
				empty := wide
				empty.Empty()
				for k := 0; k < wide.Width; k++ {
					b.setRaw(x-j+k, y, empty)
				}
				break
			}
		}
	}
	if c == nil {
		b.setRaw(x, y, uv.EmptyCell)
		return
	}
	b.setRaw(x, y, *c)
	if c.Width > lineWidth-x {
		empty := *c
		empty.Empty()
		for j := 0; j < c.Width && x+j < lineWidth; j++ {
			b.setRaw(x+j, y, empty)
		}
		return
	}
	for j := 1; j < c.Width && x+j < lineWidth; j++ {
		b.setRaw(x+j, y, uv.Cell{})
	}
}

func (b *flatScreenBuffer) Resize(width, height int) {
	oldWidth, oldHeight := b.Width(), b.Height()
	if width == oldWidth && height == oldHeight {
		return
	}
	// A one-column drag must not allocate/copy the full plane on every step.
	// Retain the physical stride while all new logical rows fit its capacity.
	if height == oldHeight && width <= b.stride {
		fits := true
		for y := range height {
			n := b.rowLength(y)
			if width > oldWidth {
				n = max(n, 0) + width - oldWidth
			} else {
				n = width
			}
			if n > b.stride {
				fits = false
				break
			}
		}
		if fits {
			blank := b.tables.pack(uv.EmptyCell)
			for y := range height {
				old := b.rowLength(y)
				next := width
				if width > oldWidth {
					next = max(old, 0) + width - oldWidth
				}
				for x := next; x < old; x++ {
					b.assign(x, y, FlatCell{})
				}
				for x := max(old, 0); x < next; x++ {
					b.assign(x, y, blank)
				}
				b.lengths[b.physical(y)] = next
				if width > oldWidth {
					b.TouchLine(oldWidth, y, width-oldWidth)
				}
			}
			b.tables.release(blank)
			return
		}
	}
	lengths := make([]int, height)
	stride := width
	for y := range min(oldHeight, height) {
		n := b.rowLength(y)
		if width > oldWidth {
			n = max(n, 0) + width - oldWidth
		} else if width < oldWidth {
			n = width
		}
		lengths[y] = n
		stride = max(stride, n)
	}
	for y := oldHeight; y < height; y++ {
		lengths[y] = width
	}
	cells := make([]FlatCell, stride*height)
	rows := make([]int, height)
	blank := b.tables.pack(uv.EmptyCell)
	for y := range height {
		rows[y] = y
		for x := 0; x < lengths[y]; x++ {
			c := blank
			if y < oldHeight && x < b.rowLength(y) {
				c = b.raw(x, y)
			}
			b.tables.retain(c)
			cells[y*stride+x] = c
		}
	}
	for _, c := range b.cells {
		b.tables.release(c)
	}
	b.tables.release(blank)
	b.cells, b.rows, b.lengths, b.stride, b.head = cells, rows, lengths, stride, 0
	if width > oldWidth {
		for y := range min(oldHeight, height) {
			b.TouchLine(oldWidth, y, width-oldWidth)
		}
	}
	for y := oldHeight; y < height; y++ {
		b.TouchLine(0, y, width)
	}
}

func (b *flatScreenBuffer) Clear() {
	blank := b.tables.pack(uv.EmptyCell)
	for y := range b.Height() {
		for x := 0; x < b.Width(); x++ {
			b.assign(x, y, blank)
		}
		b.TouchLine(0, y, b.Width())
	}
	b.tables.release(blank)
}

func (b *flatScreenBuffer) ClearArea(area uv.Rectangle) { b.FillArea(nil, area) }
func (b *flatScreenBuffer) Fill(c *uv.Cell)             { b.FillArea(c, b.Bounds()) }
func (b *flatScreenBuffer) FillArea(c *uv.Cell, area uv.Rectangle) {
	width := 1
	if c != nil {
		width = max(width, c.Width)
	}
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x += width {
			b.setCell(x, y, c, false)
		}
		b.TouchLine(area.Min.X, y, area.Dx())
	}
}

func (b *flatScreenBuffer) reverseRows(first, last int) {
	for first < last-1 {
		i, j := (b.head+first)%b.Height(), (b.head+last-1)%b.Height()
		b.rows[i], b.rows[j] = b.rows[j], b.rows[i]
		first++
		last--
	}
}

func (b *flatScreenBuffer) rotateRows(first, last, n int) {
	if first == 0 && last == b.Height() {
		b.head = (b.head + n) % b.Height()
		return
	}
	b.reverseRows(first, first+n)
	b.reverseRows(first+n, last)
	b.reverseRows(first, last)
}

func (b *flatScreenBuffer) fillRows(first, last int, c *uv.Cell) {
	if c != nil && c.Width > 1 {
		for y := first; y < last; y++ {
			for x := 0; x < b.rowLength(y); x++ {
				b.setCell(x, y, c, false)
			}
		}
		return
	}
	cell := uv.EmptyCell
	if c != nil {
		cell = *c
	}
	packed := b.tables.pack(cell)
	for y := first; y < last; y++ {
		for x := 0; x < b.rowLength(y); x++ {
			b.assign(x, y, packed)
		}
	}
	b.tables.release(packed)
}

func (b *flatScreenBuffer) InsertLineArea(y, n int, c *uv.Cell, area uv.Rectangle) {
	b.insertLines(y, n, c, area)
	// Damage uses the caller's requested count, even when movement was clamped.
	for i := area.Min.Y; i < area.Max.Y; i++ {
		b.TouchLine(area.Min.X, i, area.Dx())
		b.TouchLine(area.Min.X, i-n, area.Dx())
	}
}

func (b *flatScreenBuffer) insertLines(y, n int, c *uv.Cell, area uv.Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() {
		return
	}
	n = min(n, area.Max.Y-y)
	if area.Min.X <= 0 && area.Max.X >= b.Width() {
		b.rotateRows(y, area.Max.Y, area.Max.Y-y-n)
		b.fillRows(y, y+n, c)
	} else {
		for dst := area.Max.Y - 1; dst >= y+n; dst-- {
			for x := area.Min.X; x < area.Max.X; x++ {
				b.assign(x, dst, b.raw(x, dst-n))
			}
		}
		for dst := y; dst < y+n; dst++ {
			for x := area.Min.X; x < area.Max.X; x++ {
				b.setCell(x, dst, c, false)
			}
		}
	}
}

func (b *flatScreenBuffer) DeleteLineArea(y, n int, c *uv.Cell, area uv.Rectangle) {
	b.deleteLines(y, n, c, area)
	for i := area.Min.Y; i < area.Max.Y; i++ {
		b.TouchLine(area.Min.X, i, area.Dx())
		if shifted := i + n; shifted < area.Min.Y || shifted >= area.Max.Y {
			b.TouchLine(area.Min.X, shifted, area.Dx())
		}
	}
}

func (b *flatScreenBuffer) deleteLines(y, n int, c *uv.Cell, area uv.Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() {
		return
	}
	n = min(n, area.Max.Y-y)
	if area.Min.X <= 0 && area.Max.X >= b.Width() {
		b.rotateRows(y, area.Max.Y, n)
		b.fillRows(area.Max.Y-n, area.Max.Y, c)
	} else {
		for dst := y; dst < area.Max.Y-n; dst++ {
			for x := area.Min.X; x < area.Max.X; x++ {
				b.assign(x, dst, b.raw(x, dst+n))
			}
		}
		for dst := area.Max.Y - n; dst < area.Max.Y; dst++ {
			for x := area.Min.X; x < area.Max.X; x++ {
				b.setCell(x, dst, c, false)
			}
		}
	}
}

func (b *flatScreenBuffer) InsertCellArea(x, y, n int, c *uv.Cell, area uv.Rectangle) {
	b.insertCells(x, y, n, c, area)
	if x+n > area.Max.X {
		n = area.Max.X - x
	}
	b.TouchLine(x, y, n)
}

func (b *flatScreenBuffer) insertCells(x, y, n int, c *uv.Cell, area uv.Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() || x < area.Min.X || x >= area.Max.X || x >= b.Width() {
		return
	}
	n = min(n, area.Max.X-x)
	for dst := area.Max.X - 1; dst >= x+n && dst-n >= area.Min.X; dst-- {
		b.assign(dst, y, b.raw(dst-n, y))
	}
	for dst := x; dst < x+n && dst < area.Max.X; dst++ {
		b.setCell(dst, y, c, false)
	}
}

func (b *flatScreenBuffer) DeleteCellArea(x, y, n int, c *uv.Cell, area uv.Rectangle) {
	b.deleteCells(x, y, n, c, area)
	if n > area.Max.X-x {
		n = area.Max.X - x
	}
	b.TouchLine(x, y, n)
}

func (b *flatScreenBuffer) deleteCells(x, y, n int, c *uv.Cell, area uv.Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() || x < area.Min.X || x >= area.Max.X || x >= b.Width() {
		return
	}
	n = min(n, area.Max.X-x)
	for dst := x; dst < area.Max.X-n; dst++ {
		cell := b.tables.unpack(b.raw(dst+n, y))
		b.setCell(dst, y, &cell, false)
	}
	for dst := area.Max.X - n; dst < area.Max.X; dst++ {
		b.setCell(dst, y, c, false)
	}
}

func (b *flatScreenBuffer) String() string {
	lines := make(uv.Lines, b.Height())
	for y := range lines {
		lines[y] = b.Line(y)
	}
	return lines.String()
}
func (b *flatScreenBuffer) Render() string {
	lines := make(uv.Lines, b.Height())
	for y := range lines {
		lines[y] = b.Line(y)
	}
	return lines.Render()
}
