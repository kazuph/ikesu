package endpoint

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"image/color"
	"math"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

type flatUnsupportedColor []byte

func (flatUnsupportedColor) RGBA() (uint32, uint32, uint32, uint32) {
	return 0, 0, 0, 0
}

func TestFlatDiffUnsupportedColorReturnsError(t *testing.T) {
	cell := uv.Cell{Content: "x", Width: 1, Style: uv.Style{Fg: flatUnsupportedColor{1}}}
	in := render.Input{State: model.New(".", 80, 24), Panes: map[int]*render.PaneSnap{1: {Grid: [][]uv.Cell{{cell}}}}}
	legacy := SnapshotEncoder{LegacyGrids: true}
	if _, err := legacy.Encode(in); err == nil {
		t.Fatal("unregistered color unexpectedly serialized on the legacy path")
	}
	if _, err := flattenCells([]cellDelta{{Cell: cell}}); err == nil {
		t.Fatal("unsupported color did not return an explicit error")
	}
}

func TestFlatDiffFixedRecordsPreserveExactCells(t *testing.T) {
	cells := []cellDelta{
		{X: 0, Y: 0, Cell: uv.Cell{Content: "界", Width: 2, Style: uv.Style{Fg: ansi.IndexedColor(15), Bg: ansi.TrueColor(0x112233), Underline: uv.UnderlineCurly, UnderlineColor: ansi.BasicColor(2), Attrs: uv.AttrBold}, Link: uv.NewLink("https://example.com", "id=wide")}},
		{X: 1, Y: 0},
		{X: 2, Y: 0, Cell: uv.Cell{Content: "e\u0301", Width: 1, Style: uv.Style{Fg: color.RGBA{255, 255, 255, 255}}}},
	}
	cells = append(cells, cellDelta{X: 3, Y: 1, Cell: cells[0].Cell})
	flat, err := flattenCells(cells)
	if err != nil || len(flat.Records) != len(cells)*flatRecordBytes || len(flat.Cells) != 3 {
		t.Fatal("fixed representation lost cells or repeated dictionary entries", flat, err)
	}
	got, err := expandCells(flat)
	if err != nil || !reflect.DeepEqual(got, cells) {
		t.Fatal("flat records changed exact color/link/grapheme data", got, err)
	}
	for _, bad := range []int{-1, int(uint64(math.MaxUint32) + 1)} {
		if _, err := flattenCells([]cellDelta{{X: bad}}); err == nil {
			t.Fatal("unsupported coordinate accepted", bad)
		}
	}
}

func TestFlatDiffRejectsMalformedRecordsAndCoordinates(t *testing.T) {
	record := func(x, y, id uint32) []byte {
		b := binary.LittleEndian.AppendUint32(nil, x)
		b = binary.LittleEndian.AppendUint32(b, y)
		return binary.LittleEndian.AppendUint32(b, id)
	}
	for name, flat := range map[string]flatGridDelta{
		"truncated":    {Records: []byte{1}},
		"dictionary":   {Records: record(0, 0, 1), Cells: []uv.Cell{{}}},
		"outside grid": {Records: record(1, 0, 0), Cells: []uv.Cell{{}}},
	} {
		t.Run(name, func(t *testing.T) {
			in := render.Input{State: model.New(".", 80, 24), Panes: map[int]*render.PaneSnap{1: {}}}
			wire := flatSnapshotWire{Input: in, Grids: map[int]gridDelta{1: {Present: true, Widths: []int{1}}}, Optional: optionalPresence(in), FlatGrids: map[int]flatGridDelta{1: flat}}
			var data bytes.Buffer
			if err := gob.NewEncoder(&data).Encode(wire); err != nil {
				t.Fatal(err)
			}
			var dec SnapshotDecoder
			if _, err := dec.Decode(data.Bytes()); err == nil {
				t.Fatal("invalid flat transport accepted")
			}
		})
	}
}

func TestFlatDiffReseedsConnectionAndKeepsEarlierSnapshotsImmutable(t *testing.T) {
	in := render.Input{State: model.New(".", 80, 24), Panes: map[int]*render.PaneSnap{1: {Grid: [][]uv.Cell{{{Content: "a", Width: 1}, {Content: "界", Width: 2}, {}}}}}}
	var enc SnapshotEncoder
	var dec SnapshotDecoder
	decode := func(e *SnapshotEncoder, d *SnapshotDecoder) render.Input {
		t.Helper()
		data, err := e.Encode(in)
		if err != nil {
			t.Fatal(err)
		}
		got, err := d.Decode(data)
		if err != nil || !reflect.DeepEqual(got.Panes, in.Panes) {
			t.Fatal("connection did not receive complete snapshot", err)
		}
		return got
	}
	first := decode(&enc, &dec)
	in.Panes[1].Grid[0][0].Content = "changed"
	decode(&enc, &dec)
	var resumedEnc SnapshotEncoder
	var resumedDec SnapshotDecoder
	decode(&resumedEnc, &resumedDec)
	if first.Panes[1].Grid[0][0].Content != "a" {
		t.Fatal("later transport mutated a retained snapshot")
	}
	// A peer without the experimental codec receives the original gob shape.
	legacy := SnapshotEncoder{LegacyGrids: true}
	data, err := legacy.Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	var wire flatSnapshotWire
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&wire); err != nil || len(wire.FlatGrids) != 0 || len(wire.Grids[1].Cells) != 3 {
		t.Fatal("old display received an experimental delta", err)
	}
	if HasFlatDiff(Current()) != ExperimentalFlatDiff || HasFlatDiff(GraphicsPeer()) != ExperimentalFlatDiff {
		t.Fatal("build tag and advertised codec disagree")
	}
}
