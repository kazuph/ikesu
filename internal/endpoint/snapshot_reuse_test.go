package endpoint

import (
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

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
