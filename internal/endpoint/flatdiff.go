package endpoint

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"

	uv "github.com/charmbracelet/ultraviolet"
)

const FlatDiffCodec = "experimental-flat-cell-diff-v1"
const flatRecordBytes = 3 * 4 // uint32 x, y, dictionary index

type flatGridDelta struct {
	Records []byte
	Cells   []uv.Cell
}

func HasFlatDiff(peer Peer) bool {
	for _, codec := range peer.SurfaceCodecs {
		if codec == FlatDiffCodec {
			return true
		}
	}
	return false
}

func flattenCells(cells []cellDelta) (flatGridDelta, error) {
	flat := flatGridDelta{Records: make([]byte, 0, len(cells)*flatRecordBytes)}
	ids := make(map[uv.Cell]uint32)
	for _, change := range cells {
		if change.X < 0 || change.Y < 0 || uint64(change.X) > math.MaxUint32 || uint64(change.Y) > math.MaxUint32 {
			return flatGridDelta{}, fmt.Errorf("unsupported flat cell coordinate")
		}
		if !reflect.ValueOf(change.Cell).Comparable() {
			return flatGridDelta{}, fmt.Errorf("unsupported flat cell dictionary value")
		}
		id, ok := ids[change.Cell]
		if !ok {
			if uint64(len(flat.Cells)) > math.MaxUint32 {
				return flatGridDelta{}, fmt.Errorf("unsupported flat cell dictionary size")
			}
			id = uint32(len(flat.Cells))
			ids[change.Cell] = id
			flat.Cells = append(flat.Cells, change.Cell)
		}
		flat.Records = binary.LittleEndian.AppendUint32(flat.Records, uint32(change.X))
		flat.Records = binary.LittleEndian.AppendUint32(flat.Records, uint32(change.Y))
		flat.Records = binary.LittleEndian.AppendUint32(flat.Records, id)
	}
	return flat, nil
}

func expandCells(flat flatGridDelta) ([]cellDelta, error) {
	if len(flat.Records)%flatRecordBytes != 0 {
		return nil, fmt.Errorf("invalid flat cell record length")
	}
	cells := make([]cellDelta, 0, len(flat.Records)/flatRecordBytes)
	for offset := 0; offset < len(flat.Records); offset += flatRecordBytes {
		r := flat.Records[offset : offset+flatRecordBytes]
		id := binary.LittleEndian.Uint32(r[8:])
		if uint64(id) >= uint64(len(flat.Cells)) {
			return nil, fmt.Errorf("invalid flat cell dictionary index")
		}
		cells = append(cells, cellDelta{X: int(binary.LittleEndian.Uint32(r)), Y: int(binary.LittleEndian.Uint32(r[4:])), Cell: flat.Cells[id]})
	}
	return cells, nil
}
