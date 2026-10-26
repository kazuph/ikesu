package render

import (
	"math/rand"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
)

func TestGroupedStylesPreserveTerminalCellsAcrossWideOverwrites(t *testing.T) {
	modern, legacy := &Encoder{}, &legacyTextEncoder{}
	a, b := vt.NewEmulator(32, 6), vt.NewEmulator(32, 6)
	defer a.Close()
	defer b.Close()
	rng := rand.New(rand.NewSource(42))
	for step := 0; step < 256; step++ {
		f := NewFrame(32, 6)
		for y := 0; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				style := uv.Style{Fg: ansi.IndexedColor(rng.Intn(8)), Bg: ansi.IndexedColor(rng.Intn(8)), Attrs: uint8(rng.Intn(4)), Underline: uv.Underline(rng.Intn(6))}
				text := []string{" ", "a", "界", "é"}[rng.Intn(4)]
				if x == f.W-1 && text == "界" {
					text = "z"
				}
				f.Set(x, y, text, style)
				if text == "界" {
					x++
				}
			}
		}
		for _, output := range []struct {
			emu  *vt.Emulator
			data []byte
		}{{a, modern.Frame(f, 0, 0, false)}, {b, legacy.Frame(f, 0, 0, false)}} {
			if _, err := output.emu.Write(output.data); err != nil {
				t.Fatal(err)
			}
		}
		for y := 0; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				if !a.CellAt(x, y).Equal(b.CellAt(x, y)) {
					t.Fatalf("step%d (%d,%d): grouped=%+v ordered=%+v", step, x, y, a.CellAt(x, y), b.CellAt(x, y))
				}
			}
		}
	}
}

func BenchmarkStyledSidebarResizeEncode(b *testing.B) {
	s := model.New(".", 106, 38)
	first := s.FocusedPane()
	s.ActiveWorkspace().ActiveLayout().SplitFocused(layout.Vertical, 2)
	in := Input{State: s, Panes: map[int]*PaneSnap{first: {}, 2: {}}}
	var frames [2]*Frame
	for i := range frames {
		s.SidebarManual = true
		s.SidebarSize = 26 + i
		frames[i] = ComposeClient(in, nil, "test", false).Frame
	}
	var encoder Encoder
	encoder.Frame(frames[0], 0, 0, false)
	b.ReportAllocs()
	b.ResetTimer()
	total := 0
	for i := 0; i < b.N; i++ {
		total += len(encoder.Frame(frames[i%2], 0, 0, false))
	}
	b.StopTimer()
	b.ReportMetric(float64(total)/float64(b.N), "bytes/frame")
}
