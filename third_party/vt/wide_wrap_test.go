package vt

import "testing"

func TestWideGlyphWrapKeepsCurrentPenOnUnusedMargin(t *testing.T) {
	e := NewEmulator(5, 2)
	_, _ = e.Write([]byte("\x1b[31mxxxx界Z"))
	margin, head := e.CellAt(4, 0), e.CellAt(0, 1)
	if margin.Content != " " || margin.Width != 1 || margin.Style != head.Style || margin.Style.Fg == nil {
		t.Fatal("wide wrap lost the current pen on the unused margin", margin, head)
	}
	if head.Content != "界" || head.Width != 2 || e.CellAt(2, 1).Content != "Z" {
		t.Fatal("painting the margin changed the wrapped text")
	}
}
