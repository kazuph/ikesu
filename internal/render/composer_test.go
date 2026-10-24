package render

import (
	"bytes"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
)

func TestComposerReuseMatchesIndependentFramesAndTerminalBytes(t *testing.T) {
	state := model.New(".", 106, 38)
	first := state.FocusedPane()
	state.ActiveWorkspace().ActiveLayout().SplitFocused(layout.Vertical, 2)
	in := Input{State: state, Panes: map[int]*PaneSnap{first: {}, 2: {}}}
	var composer Composer
	var reusedEncoder, independentEncoder Encoder
	var prior *Frame
	sizes := [][2]int{{106, 38}, {160, 45}, {250, 70}, {160, 45}, {106, 38}}
	for step := range 3 * len(sizes) {
		size := sizes[step%len(sizes)]
		state.Cols, state.Rows = size[0], size[1]
		state.SidebarVisible = step%2 == 0
		state.RightSidebarMode = []string{"hidden", "compact", ""}[step%3]
		state.ActiveWorkspace().ActiveLayout().SetRatioAt(nil, []float64{.25, .5, .75}[step%3])
		in.Toast, in.Menu = nil, nil
		if step%2 == 0 {
			in.Toast = &Toast{Title: "確認", Body: "é 界", Position: "top-right"}
		} else {
			in.Menu = &Menu{Title: "操作", X: 2, Y: 2, Items: []MenuItem{{Label: "開く"}}}
		}
		in.Spin = step
		for id, pane := range in.Panes {
			pane.Grid = [][]uv.Cell{{
				{Content: string(rune('a' + step + id)), Width: 1, Link: uv.NewLink("https://example.com/"), Style: uv.Style{Fg: ansi.IndexedColor(id)}},
				{Content: "界", Width: 2, Style: uv.Style{Attrs: uv.AttrBold}}, {}, {Content: "é", Width: 1},
			}}
			pane.Cursor = CursorInfo{X: 4, Y: 0, Visible: step%2 == 0}
			pane.Graphics = nil
			if step%3 == 0 {
				pane.Graphics = []kitty.Placement{{ID: 1, X: 1, Y: 1, Cols: 2, Rows: 2, Image: kitty.Image{ID: 1, Format: 24, Width: 1, Height: 1, Data: []byte{byte(step), 2, 3}}}}
			}
		}
		var previous Frame
		if prior != nil {
			previous = *prior
			previous.Cells = append([]uv.Cell(nil), prior.Cells...)
		}
		got := composer.ComposeClient(in, nil, "test", false)
		if prior != nil && !reflect.DeepEqual(*prior, previous) {
			t.Fatal("composition changed the frame still used by the terminal encoder", step)
		}
		want := ComposeClient(in, nil, "test", false)
		if !reflect.DeepEqual(got, want) {
			t.Fatal("reused frame changed cells, images, palette, cursor, or hit regions", step)
		}
		actual := reusedEncoder.Frame(got.Frame, got.AnchorX, got.AnchorY, got.AnchorVisible, got.AnchorShape)
		expected := independentEncoder.Frame(want.Frame, want.AnchorX, want.AnchorY, want.AnchorVisible, want.AnchorShape)
		if !bytes.Equal(actual, expected) {
			t.Fatal("reused frame changed exact delta terminal output", step)
		}
		prior = got.Frame
	}
}

func BenchmarkComposedFrameReuse(b *testing.B) {
	for _, reuse := range []bool{false, true} {
		name := "independent"
		if reuse {
			name = "borrowed"
		}
		b.Run(name, func(b *testing.B) {
			state := model.New(".", 160, 45)
			in := Input{State: state, Panes: map[int]*PaneSnap{state.FocusedPane(): {}}}
			var composer Composer
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if reuse {
					composer.ComposeClient(in, nil, "test", false)
				} else {
					ComposeClient(in, nil, "test", false)
				}
			}
		})
	}
}
