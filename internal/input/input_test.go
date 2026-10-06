package input

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func drainKinds(evs []Event) []Kind {
	out := make([]Kind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func TestPlainKeys(t *testing.T) {
	f := &Framer{}
	evs := f.Push([]byte("ab\x03")) // a, b, ctrl-c
	if len(evs) != 3 {
		t.Fatalf("events = %v", drainKinds(evs))
	}
	if evs[0].Key.Code != 'a' || evs[1].Key.Code != 'b' || evs[2].Key.Code != 'c' || !evs[2].Key.Mod.Contains(uv.ModCtrl) {
		t.Fatalf("keys = %+v", evs)
	}
}

// An SGR mouse sequence split across two reads must never leak as text.
func TestSplitMouseSequence(t *testing.T) {
	f := &Framer{}
	evs := f.Push([]byte("\x1b[<35;61;8"))
	if len(evs) != 0 {
		t.Fatalf("partial seq emitted %v", drainKinds(evs))
	}
	evs = f.Push([]byte("M\x1b[<0;61;8m")) // completes + release
	if len(evs) != 2 {
		t.Fatalf("events = %v", drainKinds(evs))
	}
	if evs[0].Kind != KindMouseMotion || evs[1].Kind != KindMouseRelease {
		t.Fatalf("events = %v", drainKinds(evs))
	}
	if evs[0].Mouse.X != 60 || evs[0].Mouse.Y != 7 {
		t.Fatalf("pos = %d,%d", evs[0].Mouse.X, evs[0].Mouse.Y)
	}
}

// A truncated SGR fragment + orphan tail bytes are discarded entirely.
func TestOrphanMouseTailDiscarded(t *testing.T) {
	f := &Framer{}
	if evs := f.Push([]byte("\x1b[<35;61")); len(evs) != 0 {
		t.Fatalf("fragment emitted %v", drainKinds(evs))
	}
	if evs := f.FlushTimeout(); len(evs) != 0 {
		t.Fatalf("flush emitted %v", drainKinds(evs))
	}
	// The tail arrives as a separate read: only digits, ';' and terminator.
	if evs := f.Push([]byte(";8M")); len(evs) != 0 {
		t.Fatalf("tail emitted %v", drainKinds(evs))
	}
	// Normal input resumes afterwards.
	if evs := f.Push([]byte("x")); len(evs) != 1 || evs[0].Key.Code != 'x' {
		t.Fatalf("post-tail events = %v", drainKinds(evs))
	}
}

// A lone Escape keypress is still delivered after the idle window.
func TestLoneEscape(t *testing.T) {
	f := &Framer{}
	if evs := f.Push([]byte{0x1b}); len(evs) != 0 {
		t.Fatalf("esc emitted early")
	}
	if evs := f.FlushTimeout(); len(evs) != 0 {
		t.Fatalf("first flush emitted %v", drainKinds(evs))
	}
	evs := f.FlushTimeout()
	if len(evs) != 1 || evs[0].Key.Code != uv.KeyEscape {
		t.Fatalf("escape = %v", drainKinds(evs))
	}
}

// ESC + tail of an SGR mouse report: the held ESC plus the tail reassemble
// into the real mouse event (reassembling beats discarding — the click is
// not lost, and nothing reaches the pane as text).
func TestEscapePlusOrphanTail(t *testing.T) {
	f := &Framer{}
	f.Push([]byte{0x1b})
	f.FlushTimeout() // now escHeld
	evs := f.Push([]byte("[<35;61;8M"))
	if len(evs) != 1 || evs[0].Kind != KindMouseMotion {
		t.Fatalf("events = %v", drainKinds(evs))
	}
	if f.Pending() {
		t.Fatalf("seq should be complete: mouse release consumed; pending=%v", f.Pending())
	}
}

// An SGR tail that lost its ESC entirely is discarded, not typed.
func TestOrphanTailWithoutEscape(t *testing.T) {
	f := &Framer{}
	evs := f.Push([]byte("[<35;61;8M"))
	if len(evs) != 0 || f.Pending() {
		t.Fatalf("events = %v pending=%v", drainKinds(evs), f.Pending())
	}
	// Incomplete variant waits for the terminator, then drops at flush.
	f = &Framer{}
	if evs := f.Push([]byte("[<35;61;")); len(evs) != 0 {
		t.Fatalf("events = %v", drainKinds(evs))
	}
	if evs := f.FlushTimeout(); len(evs) != 0 {
		t.Fatalf("flush events = %v", drainKinds(evs))
	}
	if f.Pending() {
		t.Fatal("buffer should be empty")
	}
	// Plain text starting with "[<" that isn't a mouse tail still works.
	f = &Framer{}
	evs = f.Push([]byte("[<hello"))
	if len(evs) == 0 || evs[0].Kind != KindKey {
		t.Fatalf("events = %v", drainKinds(evs))
	}
}

// Bracketed paste spanning reads becomes one paste event.
func TestBracketedPaste(t *testing.T) {
	f := &Framer{}
	evs := f.Push([]byte("\x1b[200~hello "))
	if len(evs) != 0 && evs[0].Kind != KindUnknown {
		t.Fatalf("events = %v", drainKinds(evs))
	}
	evs = f.Push([]byte("wor\x1b[201~ld"))
	var paste *Event
	for i := range evs {
		if evs[i].Kind == KindPaste {
			paste = &evs[i]
		}
	}
	if paste == nil || paste.Text != "hello wor" {
		t.Fatalf("paste = %+v", evs)
	}
	// trailing "ld" is regular input
	last := evs[len(evs)-1]
	if last.Kind != KindKey || last.Key.Code != 'd' {
		t.Fatalf("tail = %+v", evs)
	}
}
