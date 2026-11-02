package vt

import (
	"image/color"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

type historySliceColor []uint8

func (c historySliceColor) RGBA() (uint32, uint32, uint32, uint32) {
	return uint32(c[0]) * 257, 0, 0, 65535
}

// Compare byte-limited storage with the original expanded-cell representation.
func TestStyledHistoryMatchesExpandedCells(t *testing.T) {
	for _, compressed := range []bool{true, false} {
		name := "expanded"
		if compressed {
			name = "compressed"
		}
		t.Run(name, func(t *testing.T) {
			row := make(uv.Line, 80)
			for x := range row {
				index := x
				if compressed {
					index = (x / 8) % 2
				}
				row[x] = uv.Cell{Content: "x", Width: 1, Style: uv.Style{
					Fg: ansi.IndexedColor(index), Bg: color.RGBA{R: 4, G: 9, B: 12, A: 255},
					UnderlineColor: ansi.TrueColor(0x123456), Underline: uv.UnderlineCurly,
					Attrs: uv.AttrBold | uv.AttrItalic,
				}, Link: uv.Link{URL: "https://example.test/色", Params: "id=1"}}
			}
			row[0].Content, row[0].Width = "界", 2
			row[1] = uv.Cell{}
			row[2].Content = "e\u0301"
			row[3].Content, row[3].Width = "👩‍💻", 2
			row[4] = uv.Cell{}
			const retained = 3
			charge := scrollbackRowBytes + len(row)*scrollbackCellBytes
			packed := NewScrollback(1)
			packed.SetMaxBytes(retained * charge)
			expanded := NewScrollback(retained)
			for n := 0; n < retained*3; n++ {
				row[5].Content = string(rune('a' + n))
				packed.Push(row)
				expanded.Push(row)
				idx := packed.wrap(packed.start + packed.count - 1)
				if (packed.styled[idx] != nil) != compressed {
					t.Fatalf("push %d: wrong storage", n)
				}
				if packed.Len() != expanded.Len() || packed.Bytes() != packed.Len()*charge {
					t.Fatalf("push %d: retained=%d/%d bytes=%d", n, packed.Len(), expanded.Len(), packed.Bytes())
				}
				for y := 0; y < packed.Len(); y++ {
					want := expanded.Line(y)
					if got := packed.Line(y); !reflect.DeepEqual(got, want) {
						t.Fatalf("push %d row %d differs", n, y)
					}
					if got := packed.lineInto(y, nil); !reflect.DeepEqual(got, want) {
						t.Fatalf("push %d row %d: lineInto differs", n, y)
					}
					for x := range want {
						if got := packed.CellAt(x, y); got == nil || !reflect.DeepEqual(*got, want[x]) {
							t.Fatalf("push %d row %d cell %d differs", n, y, x)
						}
					}
				}
			}
			for packed.Len() > 0 {
				if got, want := packed.popLast(), expanded.popLast(); !reflect.DeepEqual(got, want) {
					t.Fatal("restored row differs")
				}
			}
		})
	}
}

func TestStyledHistoryPackedTextPreservesCellsOnReuse(t *testing.T) {
	style := uv.Style{Fg: ansi.IndexedColor(23), UnderlineColor: color.RGBA{R: 31, A: 255},
		Underline: uv.UnderlineCurly, Attrs: uv.AttrBold}
	row := make(uv.Line, 32)
	for i := range row {
		row[i] = uv.Cell{Content: "色", Width: 2, Style: style, Link: uv.Link{URL: "https://example.test/界", Params: "id=1"}}
	}
	row[0].Content = ""
	row[1].Content = "e\u0301"
	row[2].Content = "\xff"
	row[3].Width = 0
	saved := packStyled(row, nil)
	if saved == nil {
		t.Fatal("fixture must use packed styled history")
	}
	before := saved.line()
	if !reflect.DeepEqual(before, row) {
		t.Fatal("packed history changed cell values")
	}
	row[1].Content = "new"
	if packStyled(row, saved) == nil {
		t.Fatal("reused row unexpectedly unpacked")
	}
	if before[1].Content != "e\u0301" || before[2].Content != "\xff" {
		t.Fatal("reusing stored history changed a previously read row")
	}
	if got := saved.line(); !reflect.DeepEqual(got, row) {
		t.Fatal("reused packed history changed cell values")
	}
	short := row[:len(row)/2]
	if packStyled(short, saved) == nil || !reflect.DeepEqual(saved.line(), short) {
		t.Fatal("shrinking a reused row changed cell values")
	}
}

func TestStyledWideContinuationsRemainZeroAcrossReadsAndReflow(t *testing.T) {
	row := make(uv.Line, 80)
	for x := 0; x < len(row); x += 2 {
		row[x] = uv.Cell{Content: "界", Width: 2, Style: uv.Style{Fg: ansi.IndexedColor(x / 8)}}
	}
	sb := NewScrollback(1)
	sb.SetMaxBytes(scrollbackRowBytes + len(row)*scrollbackCellBytes)
	sb.Push(row)
	if sb.styled[sb.start] == nil {
		t.Fatal("wide continuation cells must not force expanded storage")
	}
	if got := sb.Line(0); !reflect.DeepEqual(got, row) {
		t.Fatal("line changed")
	}
	scratch := make(uv.Line, len(row))
	for i := range scratch {
		scratch[i] = uv.Cell{Content: "stale", Width: 1}
	}
	if got := sb.lineInto(0, scratch[:0]); !reflect.DeepEqual(got, row) {
		t.Fatal("reused line buffer retained stale cells")
	}
	for x := range row {
		if got := sb.CellAt(x, 0); got == nil || !reflect.DeepEqual(*got, row[x]) {
			t.Fatalf("cell %d changed", x)
		}
	}
	logical := newReflowLogicalLine(row, &reflowShapePool{})
	for start := range row {
		got := make(uv.Line, len(row)-start)
		for i := range got {
			got[i] = uv.Cell{Content: "stale", Width: 1}
		}
		logical.copyRange(got, start, len(row))
		if !reflect.DeepEqual(got, row[start:]) {
			t.Fatalf("reflow slice starting at %d changed", start)
		}
	}
}

func TestStyledHistoryReusesAppearancesWithoutLosingLinksOrColors(t *testing.T) {
	styles := []uv.Style{{Fg: historySliceColor{23}, Attrs: uv.AttrBold},
		{Fg: ansi.IndexedColor(9), UnderlineColor: color.RGBA{B: 12, A: 255}}}
	links := []uv.Link{{URL: "https://example.test/色", Params: "id=a"}, {URL: "https://example.test/other", Params: "id=b"}}
	row := make(uv.Line, 80)
	for x := range row {
		choice := (x / 4) % 2
		row[x] = uv.Cell{Content: "x", Width: 1, Style: styles[choice], Link: links[choice]}
	}
	saved := packStyled(row, nil)
	if saved == nil || len(saved.styles) != 2 || len(saved.runs) != 20 {
		t.Fatal("repeated appearances were not shared")
	}
	if got := saved.line(); !reflect.DeepEqual(got, row) {
		t.Fatal("repeated colors or links changed")
	}
	for x := range row {
		row[x].Style, row[x].Link = styles[0], links[0]
	}
	if packStyled(row, saved) == nil || len(saved.styles) != 1 || !reflect.DeepEqual(saved.line(), row) {
		t.Fatal("reused appearances changed")
	}
	if saved.styles[:cap(saved.styles)][1].style.Fg != nil || saved.styles[:cap(saved.styles)][1].link.URL != "" {
		t.Fatal("unused appearances retain references")
	}
}

func TestStyledHistoryLongAppearancesAndRetention(t *testing.T) {
	for _, unique := range []bool{false, true} {
		row := longAppearanceRow(unique)
		charge := scrollbackRowBytes + len(row)*scrollbackCellBytes
		sb := NewScrollback(1)
		sb.SetMaxBytes(2 * charge)
		for n := 0; n < 3; n++ {
			row[0].Content = string(rune('a' + n))
			sb.Push(row)
			if sb.Len() != min(n+1, 2) || sb.Bytes() != sb.Len()*charge {
				t.Fatalf("unique=%v push=%d len=%d bytes=%d", unique, n, sb.Len(), sb.Bytes())
			}
			idx := sb.wrap(sb.start + sb.count - 1)
			if (sb.styled[idx] == nil) != unique {
				t.Fatalf("unique=%v: unexpected representation", unique)
			}
			if got := sb.Line(sb.Len() - 1); !reflect.DeepEqual(got, row) {
				t.Fatalf("unique=%v: cells differ", unique)
			}
		}
	}
}

func TestStyledHistoryRingResizeAndHandoff(t *testing.T) {
	packed, expanded := NewEmulator(80, 4), NewEmulator(80, 4)
	defer packed.Close()
	defer expanded.Close()
	packed.Scrollback().SetMaxBytes(3 * (scrollbackRowBytes + 80*scrollbackCellBytes))
	expanded.SetScrollbackSize(100)
	compare := func(stage string, a, b *Emulator) {
		t.Helper()
		if a.Scrollback().Len() != b.Scrollback().Len() || !reflect.DeepEqual(a.Scrollback().Lines(), b.Scrollback().Lines()) || a.Render() != b.Render() {
			t.Fatalf("%s: history or screen differs", stage)
		}
	}
	for n := 0; n < 12; n++ {
		width := 80
		if n%2 == 0 {
			width = 40
		}
		row := longAppearanceRow(false)[:width]
		for i := range row {
			if _, ok := row[i].Style.Fg.(historySliceColor); ok {
				row[i].Style.Fg = color.RGBA{R: 23, A: 255}
			}
		}
		row[0].Content, row[0].Width = "界", 2
		row[1] = uv.Cell{}
		row[2].Content = string(rune('a' + n))
		packed.Scrollback().Push(row)
		expanded.Scrollback().Push(row)
		for expanded.Scrollback().Len() > packed.Scrollback().Len() {
			expanded.Scrollback().dropOldest()
		}
		idx := packed.Scrollback().wrap(packed.Scrollback().start + packed.Scrollback().count - 1)
		if packed.Scrollback().styled[idx] == nil {
			t.Fatalf("push %d was not compressed", n)
		}
		compare("push", packed, expanded)
	}
	data, err := packed.MarshalHandoff()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewEmulator(1, 1)
	defer restored.Close()
	if err := restored.UnmarshalHandoff(data); err != nil {
		t.Fatal(err)
	}
	compare("handoff", packed, restored)
	if packed.Scrollback().Bytes() != restored.Scrollback().Bytes() {
		t.Fatal("handoff changed byte charge")
	}
	packed.ResizeWithReflow(40, 4, nil)
	restored.ResizeWithReflow(40, 4, nil)
	expanded.ResizeWithReflow(40, 4, nil)
	compare("resize", packed, restored)
	compare("resize expanded", packed, expanded)
	if packed.Scrollback().Bytes() != restored.Scrollback().Bytes() {
		t.Fatal("resize changed byte charge after handoff")
	}
}

func longAppearanceRow(unique bool) uv.Line {
	row := make(uv.Line, 10000)
	styles := []uv.Style{{Fg: historySliceColor{23}, Attrs: uv.AttrBold}, {Fg: ansi.IndexedColor(9)}}
	for i := range row {
		style := styles[(i/4)%len(styles)]
		if unique {
			style.Fg = ansi.TrueColor(i)
		}
		row[i] = uv.Cell{Content: "x", Width: 1, Style: style, Link: uv.Link{URL: "https://example.test/色", Params: "id=1"}}
	}
	return row
}

func BenchmarkPackStyledLongAppearances(b *testing.B) {
	for _, unique := range []bool{false, true} {
		name := "repeated"
		if unique {
			name = "unique"
		}
		b.Run(name, func(b *testing.B) {
			row := longAppearanceRow(unique)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = packStyled(row, nil)
			}
		})
	}
}
