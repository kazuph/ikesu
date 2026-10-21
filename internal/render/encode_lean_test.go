package render

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/model"
	"testing"
)

func TestLeanFrameStyleStartsEachFrame(t *testing.T) {
	e := &Encoder{}
	a, b := NewFrame(12, 2), NewFrame(12, 2)
	a.Text(0, 0, "abc", uv.Style{})
	b.Text(0, 0, "abd", uv.Style{})
	e.Frame(a, 0, 0, false)
	data := e.Frame(b, 0, 0, false)
	if !bytes.Contains(data, []byte("\x1b[m")) && !bytes.Contains(data, []byte("\x1b[0m")) {
		t.Fatalf("missing initial SGR: %q", data)
	}
}

func TestLeanFullAndDiffMatchCells(t *testing.T) {
	e := &Encoder{}
	emu := vt.NewEmulator(24, 4)
	defer emu.Close()
	for _, text := range []string{"ABC 日本 😀 e Z", "ABD 本  x e Y", "                     END"} {
		f := NewFrame(24, 4)
		f.Text(0, 0, text, uv.Style{Attrs: uv.AttrBold})
		f.Set(18, 0, "é", uv.Style{})
		f.Text(0, 1, "wide:界end", uv.Style{})
		f.Text(0, 2, "                     xyz", uv.Style{})
		f.Text(0, 3, "                     xyz", uv.Style{})
		if _, err := emu.Write(e.Frame(f, 0, 0, false)); err != nil {
			t.Fatal(err)
		}
		for y := 0; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				want, got := f.Cell(x, y), emu.CellAt(x, y)
				if got.Content != want.Content || got.Width != want.Width || !got.Style.Equal(&want.Style) {
					t.Fatalf("%q (%d,%d) got %+v want %+v", text, x, y, got, want)
				}
			}
		}
	}
}

func TestLeanComposedFrameByteComparison(t *testing.T) {
	state := model.New(".", 160, 45)
	pid := state.FocusedPane()
	in := Input{State: state, Panes: map[int]*PaneSnap{pid: {Grid: [][]uv.Cell{}}}}
	for y := 0; y < 35; y++ {
		row := make([]uv.Cell, 100)
		for x := range row {
			row[x] = uv.Cell{Content: "x", Width: 1}
		}
		in.Panes[pid].Grid = append(in.Panes[pid].Grid, row)
	}
	f := Compose(in).Frame
	oldEncoder := &legacyTextEncoder{}
	old := oldEncoder.Frame(f, 0, 0, false)
	nextEncoder := &Encoder{}
	next := nextEncoder.Frame(f, 0, 0, false)
	if len(next) >= len(old) {
		t.Errorf("full output did not shrink")
	}
	t.Logf("full frame baseline=%d candidate=%d ratio=%.6f", len(old), len(next), float64(len(next))/float64(len(old)))
	changed := Compose(in).Frame
	for x := 0; x < 90; x++ {
		changed.Set(40+x, 8, "Y", uv.Style{Fg: ansi.IndexedColor(x%2 + 1), Bg: ansi.IndexedColor(0)})
	}
	beforeDiff := oldEncoder.Frame(changed, 0, 0, false)
	afterDiff := nextEncoder.Frame(changed, 0, 0, false)
	t.Logf("diff baseline=%d candidate=%d ratio=%.6f", len(beforeDiff), len(afterDiff), float64(len(afterDiff))/float64(len(beforeDiff)))
	if len(afterDiff) >= len(beforeDiff) {
		t.Error("diff did not shrink")
	}
}

func TestLeanLegacyCellParityAndLinks(t *testing.T) {
	modern, legacy := &Encoder{}, &legacyTextEncoder{}
	a, b := vt.NewEmulator(32, 5), vt.NewEmulator(32, 5)
	defer a.Close()
	defer b.Close()
	for i, text := range []string{"日本😀ABC", "半角ｶﾅ e", "XY", "XY"} {
		f := NewFrame(32, 5)
		f.Text(0, 0, text, uv.Style{Attrs: uv.AttrBold | uv.AttrItalic, Fg: ansi.IndexedColor(2), Bg: ansi.IndexedColor(4)})
		f.Set(20, 0, "é", uv.Style{Underline: uv.UnderlineSingle})
		f.Text(28, 4, "END!", uv.Style{})
		f.Set(7, 2, "界", uv.Style{})
		if i > 0 {
			f.Set(7, 2, "x", uv.Style{})
		}
		f.Set(15, 2, "Q", uv.Style{Fg: ansi.IndexedColor(i%2 + 1)})
		f.Cell(16, 2).Link = uv.NewLink("https://example.com/" + text)
		next, old := modern.Frame(f, 0, 0, false), legacy.Frame(f, 0, 0, false)
		_, _ = a.Write(next)
		_, _ = b.Write(old)
		for y := 0; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				got, want := a.CellAt(x, y), b.CellAt(x, y)
				if got.Content != want.Content || got.Width != want.Width || !got.Style.Equal(&want.Style) {
					t.Fatalf("frame%d (%d,%d) got%+v want%+v", i, x, y, got, want)
				}
			}
		}
		if !a.CellAt(16, 2).Link.Equal(&b.CellAt(16, 2).Link) {
			t.Fatal("link-only change differs from baseline")
		}
	}
}

func TestLeanStyleTransitionsMatchLegacy(t *testing.T) {
	modern, legacy := &Encoder{}, &legacyTextEncoder{}
	a, b := vt.NewEmulator(12, 3), vt.NewEmulator(12, 3)
	defer a.Close()
	defer b.Close()
	for attrs := 0; attrs <= 255; attrs++ {
		f := NewFrame(12, 3)
		for y := 0; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				style := uv.Style{Attrs: uint8(attrs + x), Underline: uv.Underline((attrs + y) % 6)}
				if x%3 != 0 {
					style.Fg = ansi.IndexedColor((attrs + x) % 256)
				}
				if x%4 != 0 {
					style.Bg = ansi.IndexedColor((attrs + y) % 256)
				}
				if x%5 != 0 {
					style.UnderlineColor = ansi.IndexedColor((attrs + x + y) % 256)
				}
				f.Set(x, y, string(rune('a'+attrs%26)), style)
			}
		}
		_, _ = a.Write(modern.Frame(f, 0, 0, false))
		_, _ = b.Write(legacy.Frame(f, 0, 0, false))
		for y := 0; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				got, want := a.CellAt(x, y), b.CellAt(x, y)
				if got.Content != want.Content || got.Width != want.Width || !got.Style.Equal(&want.Style) {
					t.Fatalf("attrs=%d (%d,%d) got %+v want %+v", attrs, x, y, got, want)
				}
			}
		}
	}
}
