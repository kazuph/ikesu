package copybuf

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// buf builds a buffer of plain strings padded to width w.
func buf(lines []string, w int) [][]uv.Cell {
	out := make([][]uv.Cell, len(lines))
	for y, s := range lines {
		row := make([]uv.Cell, w)
		for x := range row {
			row[x] = uv.Cell{Content: " ", Width: 1}
		}
		for x, r := range []rune(s) {
			if x < w {
				row[x] = uv.Cell{Content: string(r), Width: 1}
			}
		}
		out[y] = row
	}
	return out
}

// SPEC G6 行末停止: the real text length trims trailing blanks.
func TestLineLen(t *testing.T) {
	b := buf([]string{"hello   ", "   ", ""}, 8)
	if got := LineLen(b[0]); got != 5 {
		t.Errorf("LineLen(hello) = %d; want 5", got)
	}
	if got := LineLen(b[1]); got != 0 {
		t.Errorf("LineLen(blank) = %d; want 0", got)
	}
}

// Motion semantics observed from herdr's pane.copy_motion:
// "foo bar.baz qux" at row 6 → vi-style word runs; '.' is its own word.
func TestMotionWordStarts(t *testing.T) {
	b := buf([]string{"> \"", "foo bar.baz qux", "hello   world"}, 16)
	cases := []struct {
		from   Point
		motion string
		want   Point
	}{
		{Point{1, 0}, MotionNextWordStart, Point{1, 4}},
		{Point{1, 3}, MotionNextWordStart, Point{1, 4}},
		{Point{1, 6}, MotionNextWordStart, Point{1, 7}}, // '.' is a word
		{Point{1, 9}, MotionNextWordStart, Point{1, 12}},
		{Point{1, 12}, MotionNextWordStart, Point{2, 0}}, // wraps lines
		{Point{1, 0}, MotionPreviousWordStart, Point{0, 2}},
		{Point{1, 3}, MotionPreviousWordStart, Point{1, 0}},
		{Point{1, 6}, MotionPreviousWordStart, Point{1, 4}},
		{Point{1, 9}, MotionPreviousWordStart, Point{1, 8}},
		{Point{1, 12}, MotionPreviousWordStart, Point{1, 8}},
		{Point{1, 0}, MotionNextWordEnd, Point{1, 2}},
		{Point{1, 3}, MotionNextWordEnd, Point{1, 6}},
		{Point{1, 6}, MotionNextWordEnd, Point{1, 7}},
		{Point{1, 9}, MotionNextWordEnd, Point{1, 10}},
		{Point{1, 12}, MotionNextWordEnd, Point{1, 14}},
	}
	for _, c := range cases {
		got, ok := Motion(b, c.motion, c.from.Row, c.from.Col)
		if !ok || got != c.want {
			t.Errorf("%s from %v = %v,%v; want %v", c.motion, c.from, got, ok, c.want)
		}
	}
}

// Big-word motions split only on blanks.
func TestMotionBigWords(t *testing.T) {
	b := buf([]string{"foo bar.baz qux", "hello   world"}, 16)
	cases := []struct {
		from   Point
		motion string
		want   Point
	}{
		{Point{0, 0}, MotionNextBigWordStart, Point{0, 4}},
		{Point{0, 6}, MotionNextBigWordStart, Point{0, 12}},
		{Point{0, 12}, MotionNextBigWordStart, Point{1, 0}},
		{Point{0, 9}, MotionPreviousBigWordStart, Point{0, 4}},
		{Point{0, 12}, MotionPreviousBigWordStart, Point{0, 4}},
		{Point{0, 3}, MotionNextBigWordEnd, Point{0, 10}},
		{Point{0, 9}, MotionNextBigWordEnd, Point{0, 10}},
		{Point{0, 12}, MotionNextBigWordEnd, Point{0, 14}},
	}
	for _, c := range cases {
		got, ok := Motion(b, c.motion, c.from.Row, c.from.Col)
		if !ok || got != c.want {
			t.Errorf("%s from %v = %v,%v; want %v", c.motion, c.from, got, ok, c.want)
		}
	}
}

// Paragraph motions land on the strictly next/previous blank row.
func TestMotionParagraphs(t *testing.T) {
	b := buf([]string{"a", "b", "", "c", "d", "", ""}, 8)
	if got, _ := Motion(b, MotionNextParagraph, 0, 0); got != (Point{2, 0}) {
		t.Errorf("next_paragraph from 0 = %v; want {2,0}", got)
	}
	if got, _ := Motion(b, MotionNextParagraph, 3, 0); got != (Point{5, 0}) {
		t.Errorf("next_paragraph from 3 = %v; want {5,0}", got)
	}
	if got, _ := Motion(b, MotionPreviousParagraph, 4, 0); got != (Point{2, 0}) {
		t.Errorf("previous_paragraph from 4 = %v; want {2,0}", got)
	}
	if got, _ := Motion(b, MotionPreviousParagraph, 1, 0); got != (Point{0, 0}) {
		t.Errorf("previous_paragraph from 1 = %v; want {0,0}", got)
	}
	if got, _ := Motion(b, MotionNextParagraph, 6, 0); got != (Point{6, 0}) {
		t.Errorf("next_paragraph at last = %v; want {6,0}", got)
	}
}

// Search is case-sensitive and non-overlapping.
func TestSearch(t *testing.T) {
	b := buf([]string{"aaaa bbbb", "FOO foo"}, 16)
	ms := Search(b, "aa")
	want := []Range{{Point{0, 0}, Point{0, 1}}, {Point{0, 2}, Point{0, 3}}}
	if len(ms) != 2 || ms[0] != want[0] || ms[1] != want[1] {
		t.Fatalf("aa matches = %v; want %v", ms, want)
	}
	if ms := Search(b, "FOO"); len(ms) != 1 || ms[0].Start != (Point{1, 0}) {
		t.Fatalf("FOO matches = %v", ms)
	}
	if ms := Search(b, "Foo"); len(ms) != 0 {
		t.Fatalf("Foo (case mismatch) = %v; want none", ms)
	}
	if Search(b, "") != nil {
		t.Fatal("empty query must return nil")
	}
}

// CurrentIndex: forward = first match strictly after the anchor,
// backward = last strictly before, both wrapping.
func TestCurrentIndex(t *testing.T) {
	ms := []Range{
		{Point{0, 1}, Point{0, 1}},
		{Point{0, 5}, Point{0, 5}},
		{Point{2, 0}, Point{2, 0}},
	}
	if got := CurrentIndex(ms, Point{0, 0}, true); got != 0 {
		t.Errorf("fwd from (0,0) = %d; want 0", got)
	}
	if got := CurrentIndex(ms, Point{0, 1}, true); got != 1 {
		t.Errorf("fwd on match = %d; want 1", got)
	}
	if got := CurrentIndex(ms, Point{9, 0}, true); got != 0 {
		t.Errorf("fwd past last wraps = %d; want 0", got)
	}
	// On a match, backward lands strictly before it (wraps to the last).
	if got := CurrentIndex(ms, Point{0, 1}, false); got != 2 {
		t.Errorf("bwd on first match wraps = %d; want 2", got)
	}
	if got := CurrentIndex(ms, Point{0, 5}, false); got != 0 {
		t.Errorf("bwd on 2nd match = %d; want 0", got)
	}
	if got := CurrentIndex(ms, Point{0, 0}, false); got != 2 {
		t.Errorf("bwd before first wraps = %d; want 2", got)
	}
}
