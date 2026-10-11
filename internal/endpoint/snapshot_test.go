package endpoint

import (
	"bytes"
	"image/color"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

func TestSnapshotRoundTripPreservesCellsLayoutAndChrome(t *testing.T) {
	state := model.New("/tmp", 160, 45)
	tab := state.ActiveWorkspace().TabRef()
	tab.Layout.SplitFocused(layout.Vertical, 2)
	tab.Layout.SplitFocused(layout.Horizontal, 3)
	tab.Layout.Focus(2)
	state.PaneLabels = map[int]string{2: "日本語"}
	pid := state.FocusedPane()
	in := render.Input{
		ServerUI: true, State: state, SidebarConfig: config.Config{},
		Panes: map[int]*render.PaneSnap{pid: {
			Title: "端末", Status: render.StatusGlyph{Key: "working", Color: color.RGBA{1, 2, 3, 255}},
			Grid: [][]uv.Cell{{{Content: "界", Width: 2, Style: uv.Style{Fg: ansi.IndexedColor(15), Bg: ansi.TrueColor(0x112233), UnderlineColor: ansi.BasicColor(2), Attrs: uv.AttrBold}}, {}}},
		}},
		Menu:   &render.Menu{Title: "menu", Items: []render.MenuItem{{Label: "close", Disabled: true}}},
		Dialog: &render.Dialog{Title: "confirm", Input: "名前", InputEdit: true},
		Toast:  &render.Toast{Title: "done", Body: "result", Position: "top-right"},
		Jobs:   []render.JobRow{{ID: "job-1", Color: ansi.IndexedColor(4)}},
	}
	var encoder SnapshotEncoder
	var decoder SnapshotDecoder
	for n := range 3 {
		in.Spin = n
		data, err := encoder.Encode(in)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decoder.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(in.Panes, got.Panes) || !reflect.DeepEqual(in.Menu, got.Menu) || !reflect.DeepEqual(in.Dialog, got.Dialog) || !reflect.DeepEqual(in.Toast, got.Toast) {
			t.Fatalf("snapshot contents differ: %+v", got)
		}
		if got.State.ActiveWorkspace().TabRef().Layout.Fingerprint() != tab.Layout.Fingerprint() {
			t.Fatal("layout tree/focus/order changed")
		}
		wantFrame, gotFrame := render.Compose(in), render.Compose(got)
		var wantEncoder, gotEncoder render.Encoder
		want := wantEncoder.Frame(wantFrame.Frame, wantFrame.AnchorX, wantFrame.AnchorY, wantFrame.AnchorVisible)
		actual := gotEncoder.Frame(gotFrame.Frame, gotFrame.AnchorX, gotFrame.AnchorY, gotFrame.AnchorVisible)
		if !bytes.Equal(want, actual) {
			t.Fatal("rendered terminal bytes changed after transport")
		}
	}
}

func TestSnapshotRejectsTruncatedTransport(t *testing.T) {
	var enc SnapshotEncoder
	data, err := enc.Encode(render.Input{State: model.New("/tmp", 80, 24)})
	if err != nil {
		t.Fatal(err)
	}
	var dec SnapshotDecoder
	if _, err := dec.Decode(data[:len(data)/2]); err == nil {
		t.Fatal("truncated snapshot accepted")
	}
}

func TestSnapshotDeltasPreserveChangesResizeRemovalAndPaletteIdentity(t *testing.T) {
	in := render.Input{State: model.New("/tmp", 80, 24), Panes: map[int]*render.PaneSnap{1: {}}}
	grid := make([][]uv.Cell, 20)
	for y := range grid {
		grid[y] = make([]uv.Cell, 60)
		for x := range grid[y] {
			grid[y][x] = uv.Cell{Content: "a", Width: 1}
		}
	}
	in.Panes[1].Grid = grid
	var enc SnapshotEncoder
	var dec SnapshotDecoder
	roundTrip := func() (render.Input, int) {
		t.Helper()
		data, err := enc.Encode(in)
		if err != nil {
			t.Fatal(err)
		}
		got, err := dec.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(in.Panes, got.Panes) {
			t.Fatal("delta changed pane content", in.Panes, got.Panes)
		}
		return got, len(data)
	}
	first, fullBytes := roundTrip()
	_, unchangedBytes := roundTrip()
	grid[0][0].Style.Fg = ansi.IndexedColor(15)
	_, changedBytes := roundTrip()
	if unchangedBytes >= changedBytes || changedBytes >= fullBytes {
		t.Fatal("unchanged and changed cells were not omitted", fullBytes, unchangedBytes, changedBytes)
	}
	// RGB white and palette index 15 must stay distinct on the wire.
	grid[0][0].Style.Fg = color.RGBA{255, 255, 255, 255}
	grid[0][1] = uv.Cell{Content: "界", Width: 2, Link: uv.Link{URL: "https://example.com", Params: "id=wide"}}
	grid[0][2] = uv.Cell{}
	roundTrip()
	if first.Panes[1].Grid[0][0].Style.Fg != nil {
		t.Fatal("later delta mutated an already returned snapshot")
	}
	in.Panes[1].Grid = grid[:2]
	in.Panes[1].Grid[0] = grid[0][:4]
	roundTrip()
	in.Panes[1].Grid = nil
	roundTrip()
	delete(in.Panes, 1)
	roundTrip()
	in.Panes[1] = &render.PaneSnap{Grid: [][]uv.Cell{{{Content: "restored", Width: 1}}}}
	roundTrip()
}
