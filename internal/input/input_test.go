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

// SPEC G6: a raw LF byte stays a KindKey event whose Raw is "\n" so the
// terminal path forwards the byte verbatim (decoders see ctrl+j).
func TestRawLF(t *testing.T) {
	f := &Framer{}
	evs := f.Push([]byte{'\n'})
	if len(evs) != 1 || evs[0].Kind != KindKey {
		t.Fatalf("events = %v", drainKinds(evs))
	}
	if string(evs[0].Raw) != "\n" {
		t.Fatalf("raw = %q", evs[0].Raw)
	}
}

// SPEC G6: the legacy modifyOtherKeys Shift+Enter sequence decodes as
// Enter+Shift (the TUI maps it to LF for non-kitty panes).
func TestLegacyShiftEnter(t *testing.T) {
	f := &Framer{}
	evs := f.Push([]byte("\x1b[27;2;13~"))
	if len(evs) != 1 || evs[0].Kind != KindKey {
		t.Fatalf("events = %v", drainKinds(evs))
	}
	if evs[0].Key.Code != uv.KeyEnter || evs[0].Key.Mod != uv.ModShift {
		t.Fatalf("key = %+v", evs[0].Key)
	}
}

// SPEC G6: kitty CSI-u with an empty modifier field and associated text
// equal to the key decodes as plain "a".
func TestKittyEmptyModifier(t *testing.T) {
	f := &Framer{}
	evs := f.Push([]byte("\x1b[97;;97u"))
	if len(evs) != 1 || evs[0].Kind != KindKey {
		t.Fatalf("events = %v", drainKinds(evs))
	}
	if evs[0].Key.Text != "a" || evs[0].Key.Mod != 0 {
		t.Fatalf("key = %+v", evs[0].Key)
	}
}

// SPEC G6: kitty CSI-u with a shifted key and associated shifted text
// decodes as "A" with Shift.
func TestKittyShiftedText(t *testing.T) {
	f := &Framer{}
	evs := f.Push([]byte("\x1b[97:65;2;65u"))
	if len(evs) != 1 || evs[0].Kind != KindKey {
		t.Fatalf("events = %v", drainKinds(evs))
	}
	if evs[0].Key.Text != "A" || !evs[0].Key.Mod.Contains(uv.ModShift) {
		t.Fatalf("key = %+v", evs[0].Key)
	}
}

// SPEC G6: kitty release events are never forwarded as key input.
func TestKittyReleaseDropped(t *testing.T) {
	f := &Framer{}
	evs := f.Push([]byte("\x1b[13;2:3u"))
	if len(evs) != 1 || evs[0].Kind != KindUnknown {
		t.Fatalf("events = %v", drainKinds(evs))
	}
}

// SPEC G6: undecodable keyboard-protocol sequences are dropped, never
// forwarded as keystrokes.
func TestInvalidSequenceDropped(t *testing.T) {
	for _, seq := range []string{
		"\x1b[>1;5u",   // push-flags form is not host input
		"\x1b[=1;1u",   // set-flags form is not host input
		"\x1b[?u",      // fixterm query echo is not a key
		"\x1b[97;2x",   // bad final byte: no key decode
		"\x1b[13;2:3u", // kitty release event
	} {
		f := &Framer{}
		evs := append(f.Push([]byte(seq)), f.FlushTimeout()...)
		for _, e := range evs {
			if e.Kind == KindKey {
				t.Fatalf("%q produced key event %+v", seq, e.Key)
			}
		}
	}
}
