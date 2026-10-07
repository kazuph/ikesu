package vt

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// refTrim replicates the pre-ring-buffer trailing-empty-cell scan
// (Cell.IsZero + Cell.Equal against EmptyCell) so the cheaper scan used by
// Push can be checked against it.
func refTrim(line uv.Line) int {
	lastNonEmpty := -1
	for i := len(line) - 1; i >= 0; i-- {
		c := &line[i]
		if !c.IsZero() && !c.Equal(&uv.EmptyCell) {
			lastNonEmpty = i
			break
		}
	}
	return lastNonEmpty
}

func makeLine(cells ...uv.Cell) uv.Line {
	return uv.Line(cells)
}

func TestScrollbackRingOrder(t *testing.T) {
	sb := NewScrollback(5)
	push := func(tag string) {
		sb.Push(uv.Line{{Content: tag, Width: 1}})
	}

	for i := 0; i < 12; i++ {
		push(string(rune('a' + i)))
	}
	if sb.Len() != 5 {
		t.Fatalf("Len=%d want 5", sb.Len())
	}
	// The last 5 pushes remain, oldest first: h..l.
	for i, want := range "hijkl" {
		got := sb.Line(i)
		if got == nil || got[0].Content != string(want) {
			t.Fatalf("Line(%d)=%v want %q", i, got, string(want))
		}
	}
	all := sb.Lines()
	if len(all) != 5 {
		t.Fatalf("Lines() len=%d want 5", len(all))
	}
	for i, want := range "hijkl" {
		if all[i][0].Content != string(want) {
			t.Fatalf("Lines()[%d]=%q want %q", i, all[i][0].Content, string(want))
		}
	}
	if c := sb.CellAt(0, 4); c == nil || c.Content != "l" {
		t.Fatalf("CellAt(0,4)=%v want l", c)
	}
	if sb.Line(5) != nil || sb.Line(-1) != nil {
		t.Fatal("out-of-range Line should be nil")
	}
}

func TestScrollbackRingSetMaxLines(t *testing.T) {
	sb := NewScrollback(5)
	for i := 0; i < 10; i++ { // ring wraps; remaining: f..j
		sb.Push(uv.Line{{Content: string(rune('a' + i)), Width: 1}})
	}
	sb.SetMaxLines(3)
	if sb.Len() != 3 || sb.MaxLines() != 3 {
		t.Fatalf("Len=%d Max=%d want 3/3", sb.Len(), sb.MaxLines())
	}
	for i, want := range "hij" {
		if sb.Line(i)[0].Content != string(want) {
			t.Fatalf("Line(%d)=%q want %q", i, sb.Line(i)[0].Content, string(want))
		}
	}

	sb.SetMaxLines(6) // grow keeps the same 3 lines
	for i, want := range "hij" {
		if sb.Line(i)[0].Content != string(want) {
			t.Fatalf("after grow Line(%d)=%q want %q", i, sb.Line(i)[0].Content, string(want))
		}
	}
	sb.Push(uv.Line{{Content: "k", Width: 1}})
	if got := sb.Line(3); got == nil || got[0].Content != "k" {
		t.Fatalf("Line(3)=%v want k", got)
	}

	sb.Clear()
	if sb.Len() != 0 {
		t.Fatalf("Len=%d want 0 after Clear", sb.Len())
	}
	sb.Push(uv.Line{{Content: "z", Width: 1}})
	if sb.Line(0)[0].Content != "z" {
		t.Fatalf("push after clear: %q", sb.Line(0)[0].Content)
	}
}

func TestScrollbackTrimEquivalence(t *testing.T) {
	blank := uv.EmptyCell
	styled := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Attrs: uv.AttrReverse}}
	linked := uv.Cell{Content: " ", Width: 1, Link: uv.Link{URL: "x"}}
	wide := uv.Cell{Content: "世", Width: 2}
	zero := uv.Cell{}
	text := uv.Cell{Content: "a", Width: 1}

	lines := []uv.Line{
		makeLine(text, blank, blank, blank),
		makeLine(text, blank, styled, blank, blank),
		makeLine(blank, blank, blank),
		makeLine(zero, zero, zero),
		makeLine(text, wide, zero, blank, blank),
		makeLine(text, linked, blank),
		makeLine(styled),
		makeLine(),
		makeLine(text, text, zero, zero),
	}
	for i, line := range lines {
		want := refTrim(line)
		sb := NewScrollback(10)
		sb.Push(line)
		got := sb.Line(0)
		if want < 0 {
			if len(got) != 0 {
				t.Fatalf("line %d: len=%d want empty", i, len(got))
			}
			continue
		}
		if len(got) != want+1 {
			t.Fatalf("line %d: trimmed len=%d want %d", i, len(got), want+1)
		}
		for x := 0; x <= want; x++ {
			if got[x] != line[x] {
				t.Fatalf("line %d cell %d: got %+v want %+v", i, x, got[x], line[x])
			}
		}
	}
}
