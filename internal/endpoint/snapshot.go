package endpoint

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/render"
)

func init() {
	// Preserve terminal palette indexes as indexes rather than approximating
	// them with RGB. Nil continues to mean the host terminal's default color.
	gob.Register(color.RGBA{})
	gob.Register(color.NRGBA{})
	gob.Register(color.RGBA64{})
	gob.Register(color.NRGBA64{})
	gob.Register(ansi.BasicColor(0))
	gob.Register(ansi.IndexedColor(0))
	gob.Register(ansi.TrueColor(0))
}

// SnapshotEncoder retains wire type definitions for the lifetime of one
// connection. The returned bytes must be consumed before the next call.
type SnapshotEncoder struct {
	data  bytes.Buffer
	enc   *gob.Encoder
	grids map[int][][]uv.Cell
}

func (e *SnapshotEncoder) Encode(in render.Input) ([]byte, error) {
	if e.enc == nil {
		e.enc = gob.NewEncoder(&e.data)
	}
	e.data.Reset()
	if e.grids == nil {
		e.grids = map[int][][]uv.Cell{}
	}
	wire := snapshotWire{Input: in, Grids: map[int]gridDelta{}}
	wire.Input.Panes = make(map[int]*render.PaneSnap, len(in.Panes))
	for id, pane := range in.Panes {
		if pane == nil {
			return nil, fmt.Errorf("unsupported nil pane snapshot")
		}
		value := *pane
		value.Grid = nil
		wire.Input.Panes[id] = &value
		grid := pane.Grid
		old := e.grids[id]
		delta := gridDelta{Present: grid != nil, Widths: make([]int, len(grid))}
		for y, row := range grid {
			delta.Widths[y] = len(row)
			for x := range row {
				if y >= len(old) || x >= len(old[y]) || old[y][x] != row[x] {
					delta.Cells = append(delta.Cells, cellDelta{X: x, Y: y, Cell: row[x]})
				}
			}
		}
		wire.Grids[id] = delta
		cached, err := applyGridDelta(old, delta, false)
		if err != nil {
			return nil, err
		}
		e.grids[id] = cached
	}
	for id := range e.grids {
		if _, ok := in.Panes[id]; !ok {
			delete(e.grids, id)
		}
	}
	if err := e.enc.Encode(wire); err != nil {
		return nil, err
	}
	return e.data.Bytes(), nil
}

type SnapshotDecoder struct {
	data  bytes.Buffer
	dec   *gob.Decoder
	grids map[int][][]uv.Cell
}

func (d *SnapshotDecoder) Decode(data []byte) (render.Input, error) {
	if d.dec == nil {
		d.dec = gob.NewDecoder(&d.data)
	}
	d.data.Write(data)
	var wire snapshotWire
	if err := d.dec.Decode(&wire); err != nil {
		return render.Input{}, err
	}
	if d.grids == nil {
		d.grids = map[int][][]uv.Cell{}
	}
	for id, delta := range wire.Grids {
		grid, err := applyGridDelta(d.grids[id], delta, true)
		if err != nil {
			return render.Input{}, err
		}
		pane := wire.Input.Panes[id]
		if pane == nil {
			return render.Input{}, fmt.Errorf("pane cells without pane state")
		}
		d.grids[id] = grid
		pane.Grid = grid
	}
	for id := range d.grids {
		if _, ok := wire.Input.Panes[id]; !ok {
			delete(d.grids, id)
		}
	}
	return wire.Input, nil
}

type snapshotWire struct {
	Input render.Input
	Grids map[int]gridDelta
}

type gridDelta struct {
	Present bool
	Widths  []int
	Cells   []cellDelta
}

type cellDelta struct {
	X, Y int
	Cell uv.Cell
}

// Received snapshots stay immutable: only changed rows are copied. The
// encoder's private comparison cache can update those cells in place.
func applyGridDelta(grid [][]uv.Cell, delta gridDelta, immutable bool) ([][]uv.Cell, error) {
	if !delta.Present {
		if len(delta.Cells) != 0 {
			return nil, fmt.Errorf("cells in absent pane grid")
		}
		return nil, nil
	}
	if immutable || grid == nil || len(grid) != len(delta.Widths) {
		next := make([][]uv.Cell, len(delta.Widths))
		copy(next, grid)
		grid = next
	}
	owned := make([]bool, len(grid))
	for y, width := range delta.Widths {
		if width < 0 {
			return nil, fmt.Errorf("invalid pane row width")
		}
		if len(grid[y]) != width {
			next := make([]uv.Cell, width)
			copy(next, grid[y])
			grid[y] = next
			owned[y] = true
		}
	}
	for _, change := range delta.Cells {
		if change.Y < 0 || change.Y >= len(grid) || change.X < 0 || change.X >= len(grid[change.Y]) {
			return nil, fmt.Errorf("invalid pane cell position")
		}
		if immutable && !owned[change.Y] {
			grid[change.Y] = append([]uv.Cell(nil), grid[change.Y]...)
			owned[change.Y] = true
		}
		grid[change.Y][change.X] = change.Cell
	}
	return grid, nil
}
