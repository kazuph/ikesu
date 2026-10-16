package uv

// RenderBuffer represents a buffer that keeps track of the current and new
// state of the screen, allowing for efficient rendering by only updating the
// parts of the screen that have changed.
type RenderBuffer struct {
	*Buffer

	// Touched records which lines changed since the last render. It holds at
	// least one entry per screen row, and a nil entry means that row is
	// unchanged. Shortening it is not meaningful, since the rows still exist, so
	// the renderer restores the length rather than checking it at every read.
	Touched []*LineData
}

// NewRenderBuffer creates a new [RenderBuffer] with the given width and height.
func NewRenderBuffer(width, height int) *RenderBuffer {
	return &RenderBuffer{
		Buffer:  NewBuffer(width, height),
		Touched: make([]*LineData, height),
	}
}

// TouchLine marks a line n times starting at the given x position as touched.
func (b *RenderBuffer) TouchLine(x, y, n int) {
	if y < 0 || y >= len(b.Lines) {
		return
	}

	b.growTouched()

	ch := b.Touched[y]
	if ch == nil {
		ch = &LineData{FirstCell: x, LastCell: x + n}
	} else {
		ch.FirstCell = min(ch.FirstCell, x)
		ch.LastCell = max(ch.LastCell, x+n)
	}
	b.Touched[y] = ch
}

// Touch marks the cell at the given x, y position as touched.
func (b *RenderBuffer) Touch(x, y int) {
	b.TouchLine(x, y, 0)
}

// TouchedLines returns the number of touched lines in the buffer.
func (b *RenderBuffer) TouchedLines() int {
	count := 0
	for _, t := range b.Touched {
		if t != nil {
			count++
		}
	}
	return count
}

// SetCell sets the cell at the given x, y position and marks the line as
// touched.
func (b *RenderBuffer) SetCell(x, y int, c *Cell) {
	if p := b.CellAt(x, y); !cellEqual(p, c) {
		width := 1
		if c != nil && c.Width > 0 {
			width = c.Width
		}
		if p != nil && p.Width > 0 {
			width = max(width, p.Width)
		}
		b.TouchLine(x, y, width)
	}
	b.Buffer.SetCell(x, y, c)
}

// InsertLine inserts n lines at the given line position, with the given
// optional cell, within the specified rectangles. If no rectangles are
// specified, it inserts lines in the entire buffer. Only cells within the
// rectangle's horizontal bounds are affected. Lines are pushed out of the
// rectangle bounds and lost. This follows terminal [ansi.IL] behavior.
func (b *RenderBuffer) InsertLine(y, n int, c *Cell) {
	b.InsertLineArea(y, n, c, b.Bounds())
}

// InsertLineArea inserts new lines at the given line position, with the given
// optional cell, within the rectangle bounds. Only cells within the
// rectangle's horizontal bounds are affected. Lines are pushed out of the
// rectangle bounds and lost. This follows terminal [ansi.IL] behavior.
func (b *RenderBuffer) InsertLineArea(y, n int, c *Cell, area Rectangle) {
	b.Buffer.InsertLineArea(y, n, c, area)
	for i := area.Min.Y; i < area.Max.Y; i++ {
		b.TouchLine(area.Min.X, i, area.Max.X-area.Min.X)
		b.TouchLine(area.Min.X, i-n, area.Max.X-area.Min.X)
	}
}

// DeleteLine deletes n lines at the given line position, with the given
// optional cell, within the specified rectangles. If no rectangles are
// specified, it deletes lines in the entire buffer.
func (b *RenderBuffer) DeleteLine(y, n int, c *Cell) {
	b.DeleteLineArea(y, n, c, b.Bounds())
}

// DeleteLineArea deletes lines at the given line position, with the given
// optional cell, within the rectangle bounds. Only cells within the
// rectangle's bounds are affected. Lines are shifted up within the bounds and
// new blank lines are created at the bottom. This follows terminal [ansi.DL]
// behavior.
func (b *RenderBuffer) DeleteLineArea(y, n int, c *Cell, area Rectangle) {
	b.Buffer.DeleteLineArea(y, n, c, area)
	for i := area.Min.Y; i < area.Max.Y; i++ {
		b.TouchLine(area.Min.X, i, area.Max.X-area.Min.X)
		b.TouchLine(area.Min.X, i+n, area.Max.X-area.Min.X)
	}
}

// InsertCell inserts new cells at the given position, with the given optional
// cell, within the specified rectangles. If no rectangles are specified, it
// inserts cells in the entire buffer. This follows terminal [ansi.ICH]
// behavior.
func (b *RenderBuffer) InsertCell(x, y, n int, c *Cell) {
	b.InsertCellArea(x, y, n, c, b.Bounds())
}

// InsertCellArea inserts new cells at the given position, with the given
// optional cell, within the rectangle bounds. Only cells within the
// rectangle's bounds are affected, following terminal [ansi.ICH] behavior.
func (b *RenderBuffer) InsertCellArea(x, y, n int, c *Cell, area Rectangle) {
	b.Buffer.InsertCellArea(x, y, n, c, area)
	if x+n > area.Max.X {
		n = area.Max.X - x
	}
	b.TouchLine(x, y, n)
}

// DeleteCell deletes cells at the given position, with the given optional
// cell, within the specified rectangles. If no rectangles are specified, it
// deletes cells in the entire buffer. This follows terminal [ansi.DCH]
// behavior.
func (b *RenderBuffer) DeleteCell(x, y, n int, c *Cell) {
	b.DeleteCellArea(x, y, n, c, b.Bounds())
}

// DeleteCellArea deletes cells at the given position, with the given
// optional cell, within the rectangle bounds. Only cells within the
// rectangle's bounds are affected, following terminal [ansi.DCH] behavior.
func (b *RenderBuffer) DeleteCellArea(x, y, n int, c *Cell, area Rectangle) {
	b.Buffer.DeleteCellArea(x, y, n, c, area)
	remainingCells := area.Max.X - x
	if n > remainingCells {
		n = remainingCells
	}
	b.TouchLine(x, y, n)
}

// Resize resizes the buffer and touches every cell it creates, since the
// screen there still shows whatever the last render left.
func (b *RenderBuffer) Resize(width, height int) {
	curWidth, curHeight := b.Width(), b.Height()
	b.Buffer.Resize(width, height)

	if width > curWidth {
		for y := range min(curHeight, height) {
			b.TouchLine(curWidth, y, width-curWidth)
		}
	}
	for y := curHeight; y < height; y++ {
		b.TouchLine(0, y, width)
	}
}

// Clear clears the buffer with space cells and marks all lines as touched.
func (b *RenderBuffer) Clear() {
	b.Buffer.Clear()
	w := b.Width()
	for y := range b.Lines {
		b.TouchLine(0, y, w)
	}
}

// ClearArea clears the buffer with space cells within the specified rectangle
// and marks the affected lines as touched.
func (b *RenderBuffer) ClearArea(area Rectangle) {
	b.Buffer.ClearArea(area)
	w := area.Max.X - area.Min.X
	for y := area.Min.Y; y < area.Max.Y; y++ {
		b.TouchLine(area.Min.X, y, w)
	}
}

// Fill fills the buffer with the given cell and marks all lines as touched.
func (b *RenderBuffer) Fill(c *Cell) {
	b.FillArea(c, b.Bounds())
}

// FillArea fills the buffer with the given cell within the specified rectangle
// and marks the affected lines as touched.
func (b *RenderBuffer) FillArea(c *Cell, area Rectangle) {
	b.Buffer.FillArea(c, area)
	w := area.Max.X - area.Min.X
	for y := area.Min.Y; y < area.Max.Y; y++ {
		b.TouchLine(area.Min.X, y, w)
	}
}

// growTouched restores the one-entry-per-row invariant, which an application is
// free to break by shortening or dropping the list.
//
// Only ever grows. Truncating to the screen would be the tidier rule, but
// [RenderBuffer.TouchedLines] counts non-nil records rather than touched ones,
// so discarding the entries of a frame that collapsed makes it look untouched
// and skips the erase that clears the rows it gave up. Growing is enough: every
// reader indexes by screen row, and a longer list has an entry for each.
func (b *RenderBuffer) growTouched() {
	if len(b.Touched) >= len(b.Lines) {
		return
	}
	touched := make([]*LineData, len(b.Lines))
	copy(touched, b.Touched)
	b.Touched = touched
}
