package endpoint

import (
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func TestSnapshotNarrowRowsRemainImmutableThroughChangesAndGrowth(t *testing.T) {
	var enc SnapshotEncoder
	var dec SnapshotDecoder
	input := render.Input{State: model.New(".", 160, 45), Panes: map[int]*render.PaneSnap{1: {Grid: [][]uv.Cell{{{Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "c", Width: 1}}}}}}
	decode := func() render.Input {
		t.Helper()
		data, err := enc.Encode(input)
		if err != nil {
			t.Fatal(err)
		}
		got, err := dec.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	full := decode()
	input.Panes[1].Grid[0] = input.Panes[1].Grid[0][:2]
	narrow := decode()
	if row := narrow.Panes[1].Grid[0]; len(row) != 2 || cap(row) != 2 || row[1].Content != "b" {
		t.Fatal("narrow row exposed removed cells", row, cap(row))
	}
	input.Panes[1].Grid[0][0].Content = "x"
	changed := decode()
	input.Panes[1].Grid[0] = append(input.Panes[1].Grid[0], uv.Cell{Content: "z", Width: 1})
	grown := decode()
	if full.Panes[1].Grid[0][0].Content != "a" || full.Panes[1].Grid[0][2].Content != "c" || narrow.Panes[1].Grid[0][0].Content != "a" || len(narrow.Panes[1].Grid[0]) != 2 || changed.Panes[1].Grid[0][0].Content != "x" || len(changed.Panes[1].Grid[0]) != 2 || grown.Panes[1].Grid[0][2].Content != "z" {
		t.Fatal("later decode changed an earlier snapshot")
	}
}

func BenchmarkSnapshotGridResizeDecode(b *testing.B) {
	var enc SnapshotEncoder
	var dec SnapshotDecoder
	input := render.Input{State: model.New(".", 160, 45), Panes: map[int]*render.PaneSnap{}}
	for id := 1; id <= 2; id++ {
		grid := make([][]uv.Cell, 45)
		for y := range grid {
			grid[y] = make([]uv.Cell, 160/2)
			for x := range grid[y] {
				grid[y][x] = uv.Cell{Content: "x", Width: 1}
			}
		}
		input.Panes[id] = &render.PaneSnap{Grid: grid}
	}
	seed, err := enc.Encode(input)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := dec.Decode(seed); err != nil {
		b.Fatal(err)
	}
	var data [2][]byte
	for i := range data {
		for id, pane := range input.Panes {
			for y := range pane.Grid {
				pane.Grid[y] = pane.Grid[y][:160/2-(i+id)%2]
			}
		}
		encoded, err := enc.Encode(input)
		if err != nil {
			b.Fatal(err)
		}
		data[i] = append([]byte(nil), encoded...)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := dec.Decode(data[i%len(data)]); err != nil {
			b.Fatal(err)
		}
	}
}

func TestSnapshotScratchReusePreservesMultiplePanesAndEarlierFrames(t *testing.T) {
	in := render.Input{State: model.New(".", 160, 45), Panes: map[int]*render.PaneSnap{}}
	var enc SnapshotEncoder
	var dec SnapshotDecoder
	var saved render.Input
	for step := range 12 {
		for id := 1; id <= 3; id++ {
			grid := make([][]uv.Cell, 45-step%3)
			for y := range grid {
				grid[y] = make([]uv.Cell, 160/id-step%2)
				for x := range grid[y] {
					grid[y][x] = uv.Cell{Content: string(rune('a' + id + step)), Width: 1}
				}
			}
			in.Panes[id] = &render.PaneSnap{Grid: grid}
		}
		if step%2 != 0 {
			delete(in.Panes, 2)
		}
		data, err := enc.Encode(in)
		if err != nil {
			t.Fatal(err)
		}
		got, err := dec.Decode(data)
		if err != nil || !reflect.DeepEqual(got.Panes, in.Panes) {
			t.Fatal("scratch changed pane cells or dimensions", step, err)
		}
		if step == 0 {
			saved = got
		} else if saved.Panes[1].Grid[0][0].Content != "b" || len(saved.Panes[2].Grid[0]) != 80 {
			t.Fatal("reused scratch changed an earlier snapshot")
		}
	}
}

func BenchmarkSnapshotGridResizeEncode(b *testing.B) {
	for _, changed := range []bool{false, true} {
		name := "unchanged"
		if changed {
			name = "resize"
		}
		b.Run(name, func(b *testing.B) {
			in := render.Input{State: model.New(".", 160, 45), Panes: map[int]*render.PaneSnap{}}
			for id := 1; id <= 2; id++ {
				grid := make([][]uv.Cell, 45)
				for y := range grid {
					grid[y] = make([]uv.Cell, 160/2)
					for x := range grid[y] {
						grid[y][x] = uv.Cell{Content: "x", Width: 1}
					}
				}
				in.Panes[id] = &render.PaneSnap{Grid: grid}
			}
			var enc SnapshotEncoder
			if _, err := enc.Encode(in); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if changed {
					for id, p := range in.Panes {
						for y := range p.Grid {
							p.Grid[y] = p.Grid[y][:160/2-(i+id)%2]
						}
					}
				}
				if _, err := enc.Encode(in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
