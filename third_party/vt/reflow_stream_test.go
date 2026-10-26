package vt

import (
	"image/color"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestReflowStreamingHistoryBudgetCursorAndContinuation(t *testing.T) {
	for _, limited := range []bool{false, true} {
		s := NewScreen(6, 2)
		if limited {
			s.scrollback.SetMaxBytes(2 * (scrollbackRowBytes + 6*scrollbackCellBytes))
		}
		line := func(text string) uv.Line {
			out := make(uv.Line, len(text))
			for x, r := range text {
				out[x] = uv.Cell{Content: string(r), Width: 1}
			}
			return out
		}
		s.scrollback.Push(line("abcdef"))
		s.scrollback.Push(line("ghijkl"))
		s.writeScreenLine(0, line("mnopqr"))
		s.writeScreenLine(1, line("ST"))
		s.cur.X, s.cur.Y = 2, 1
		drops := 0
		s.scrollback.OnDrop = func() { drops++ }
		flags := s.reflow(4, 2, []ReflowRow{{Width: 6}, {Continued: true, Width: 6}, {Continued: true, Width: 6}, {Continued: true, Width: 2}}, false)
		want := []uv.Line{line("abcd"), line("efgh"), line("ijkl"), line("mnop")}
		if limited {
			want = want[2:]
			if drops != 2 || s.scrollback.Bytes() != 2*(scrollbackRowBytes+4*scrollbackCellBytes) {
				t.Fatalf("drops=%d charged=%d", drops, s.scrollback.Bytes())
			}
		}
		if !reflect.DeepEqual(s.scrollback.Lines(), want) {
			t.Fatalf("limited=%v history=%v want=%v", limited, s.scrollback.Lines(), want)
		}
		if !reflect.DeepEqual(s.buf.Line(0), line("qrST")) || !s.isLineEmpty(s.buf.Line(1)) || s.cur.X != 0 || s.cur.Y != 1 {
			t.Fatalf("screen=%v cursor=%v", s.buf, s.cur.Position)
		}
		if len(flags) != len(want)+2 || flags[len(flags)-1] != (ReflowRow{Continued: true}) {
			t.Fatalf("flags=%v", flags)
		}
	}
}

func TestReflowScratchPreservesPackedStyledAndDenseRows(t *testing.T) {
	style := uv.Style{Fg: color.RGBA{R: 255, A: 255}}
	link := uv.Link{URL: "https://example.com"}
	rows := []uv.Line{
		{{Content: "a", Width: 1}, {Content: "b", Width: 1}},
		{{Content: "e\u0301", Width: 1, Style: style, Link: link}, {Content: "界", Width: 2, Style: style, Link: link}, {}},
		{{Content: "x", Width: 1, Style: style}, {Content: "y", Width: 1, Link: link}},
		nil,
	}
	for _, limited := range []bool{false, true} {
		s := NewScrollback(len(rows))
		if limited {
			s.SetMaxBytes(len(rows) * (scrollbackRowBytes + 3*scrollbackCellBytes))
		}
		for _, row := range rows {
			s.Push(row)
		}
		var scratch uv.Line
		for y := 0; y < s.Len(); y++ {
			scratch = s.lineInto(y, scratch)
			want := s.Line(y)
			if len(scratch) != len(want) || !reflect.DeepEqual([]uv.Cell(scratch), []uv.Cell(want)) && len(want) > 0 {
				t.Fatalf("row=%d got=%v want=%v", y, scratch, want)
			}
			if len(scratch) > 0 {
				scratch[0].Content = "changed"
				if s.Line(y)[0].Content == "changed" {
					t.Fatal("scratch aliases saved row")
				}
			}
		}
	}
}
