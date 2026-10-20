package endpoint

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"testing"
)

func TestSnapshotTextPreservesPixelsAndPatchBase(t *testing.T) {
	state := model.New(".", 80, 24)
	pid := state.FocusedPane()
	pixels := bytes.Repeat([]byte{1, 2, 3, 255}, imageCompressionMinimum/4)
	img := kitty.Image{ID: 1, Format: 32, Width: 512, Height: 256, Data: pixels}
	in := render.Input{State: state, Panes: map[int]*render.PaneSnap{pid: {Grid: [][]uv.Cell{{{Content: "a", Width: 1}}}, Graphics: []kitty.Placement{{ID: 1, Image: img}}}}}
	enc := SnapshotEncoder{GraphicsPatches: true}
	dec := SnapshotDecoder{GraphicsPatches: true}
	wire, err := enc.Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dec.Decode(wire); err != nil {
		t.Fatal(err)
	}
	base := enc.bases[graphicCacheKey{pid, 1}]
	for _, text := range []string{"b", "c", "c"} {
		p := *in.Panes[pid]
		p.Grid = [][]uv.Cell{{{Content: text, Width: 1}}}
		p.Graphics = append([]kitty.Placement(nil), p.Graphics...)
		p.Graphics[0].Image.Data = nil
		next := in
		next.Panes = map[int]*render.PaneSnap{pid: &p}
		wire, err = enc.EncodeText(next)
		if err != nil {
			t.Fatal(err)
		}
		if enc.grids[pid][0][0].Content == text && text != "c" {
			t.Fatal("uncommitted baseline advanced")
		}
		got, err := dec.Decode(wire)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.GraphicsPatches) != 0 || dec.MissingGraphicsKey != nil || !bytes.Equal(got.Panes[pid].Graphics[0].Image.Data, pixels) {
			t.Fatal("text changed image or required control")
		}
		if count, _ := dec.GraphicsPayloadSize(pid, 0); count != 0 {
			t.Fatal("text carried pixels", count)
		}
		if !bytes.Equal(enc.bases[graphicCacheKey{pid, 1}].Data, base.Data) {
			t.Fatal("text advanced patch base")
		}
		enc.CommitText()
	}
	changed := bytes.Clone(pixels)
	changed[0]++
	in.Panes[pid].Graphics[0].Image.Data = changed
	wire, err = enc.Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := dec.Decode(wire)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GraphicsPatches) == 0 || dec.MissingGraphicsKey != nil || !bytes.Equal(got.Panes[pid].Graphics[0].Image.Data, changed) {
		t.Fatal("patch after text not reconstructed")
	}
	in.Panes[pid].Graphics = nil
	wire, err = enc.Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dec.Decode(wire); err != nil {
		t.Fatal(err)
	}
	if len(dec.bases) != 0 || len(dec.retainedImages) != 0 {
		t.Fatal("ordinary image removal retained stale pixels")
	}

}
