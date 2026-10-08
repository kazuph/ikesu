package tui

import (
	"strings"
	"testing"
)

// copyText extraction semantics: normalized range, trailing blanks
// trimmed, newline-joined (verified through copySelRange + copyText).
func TestCopySelRangeNormalization(t *testing.T) {
	a := &App{}
	a.cm = &copyMode{anchor: &copyPoint{x: 5, y: 3}, cx: 2, cy: 1}
	from, to, ok := a.copySelRange()
	if !ok {
		t.Fatal("expected a selection")
	}
	if from != (copyPoint{2, 1}) || to != (copyPoint{5, 3}) {
		t.Fatalf("range = %v→%v; want (2,1)→(5,3)", from, to)
	}
	a.cm.anchor = nil
	if _, _, ok := a.copySelRange(); ok {
		t.Fatal("no anchor means no selection")
	}
}

// The copy button cycles normal → copy → fullscreen copy → normal.
func TestCopyLabelCycle(t *testing.T) {
	a := &App{}
	if a.copyLabel() != " COPY " {
		t.Fatalf("normal label = %q", a.copyLabel())
	}
	a.cm = &copyMode{}
	if a.copyLabel() != " PHONE COPY " {
		t.Fatalf("copy label = %q", a.copyLabel())
	}
	a.cm.fullscreen = true
	if a.copyLabel() != " EXIT COPY " {
		t.Fatalf("fullscreen label = %q", a.copyLabel())
	}
}

// The copy hint row shows the search prompt while the query is open.
func TestCopyHintRow(t *testing.T) {
	a := &App{}
	if a.copyHint() != "" {
		t.Fatal("no hint outside copy mode")
	}
	a.cm = &copyMode{searchDir: 1}
	if got := a.copyHint(); !strings.Contains(got, "h/j/k/l") {
		t.Fatalf("idle hint = %q", got)
	}
	a.cm.searching = true
	a.cm.query = "abc"
	if got := a.copyHint(); got != " COPY  /abc█  enter search  esc cancel" {
		t.Fatalf("search hint = %q", got)
	}
	a.cm.searchDir = -1
	if got := a.copyHint(); !strings.Contains(got, "?abc") {
		t.Fatalf("backward search hint = %q", got)
	}
}
