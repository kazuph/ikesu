package endpoint

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/render"
)

// EncodeText preserves image caches and defers the text delta baseline until
// the writer confirms delivery. The gob stream is connection-scoped; a failed
// write closes that connection rather than attempting to reuse its stream.
func (e *SnapshotEncoder) EncodeText(in render.Input) ([]byte, error) {
	old := e.grids
	e.grids = make(map[int][][]uv.Cell, len(old))
	for id, grid := range old {
		e.grids[id] = grid
	}
	e.textOnly = true
	data, err := e.Encode(in)
	e.textOnly = false
	e.pendingGrids = e.grids
	e.grids = old
	return data, err
}

func (e *SnapshotEncoder) CommitText() { e.grids = e.pendingGrids; e.pendingGrids = nil }
