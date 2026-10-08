package vt

import (
	"bytes"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

// expectResponse starts draining the emulator's input pipe and returns a
// channel that yields the first chunk written to it.
func expectResponse(e *Emulator) chan []byte {
	ch := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 256)
		n, err := e.pr.Read(buf)
		if err != nil {
			n = 0
		}
		ch <- buf[:n]
	}()
	return ch
}

func waitResponse(t *testing.T, ch chan []byte) []byte {
	t.Helper()
	select {
	case b := <-ch:
		return b
	case <-time.After(2 * time.Second):
		t.Fatal("no response written")
		return nil
	}
}

// SPEC G6: a FixTerm keyboard query (CSI ? u) is answered with
// CSI ? 0 u — supported kitty flags: none.
func TestFixTermQueryResponds(t *testing.T) {
	e := NewEmulator(80, 24)
	defer e.Close()
	ch := expectResponse(e)
	if _, err := e.WriteString("\x1b[?u"); err != nil {
		t.Fatal(err)
	}
	if got, want := waitResponse(t, ch), []byte("\x1b[?0u"); !bytes.Equal(got, want) {
		t.Fatalf("response = %q, want %q", got, want)
	}
}

// SPEC G6: an explicit kitty protocol push (CSI > u) marks the pane as
// protocol-aware; CSI < u pops it again.
func TestKbdProtocolStack(t *testing.T) {
	e := NewEmulator(80, 24)
	defer e.Close()
	if e.KbdExplicit() {
		t.Fatal("kbd stack should start empty")
	}
	_, _ = e.WriteString("\x1b[>1u")
	if !e.KbdExplicit() {
		t.Fatal("CSI > 1 u should mark explicit protocol")
	}
	_, _ = e.WriteString("\x1b[>5u")
	_, _ = e.WriteString("\x1b[<u")
	if !e.KbdExplicit() {
		t.Fatal("one pop should leave one entry")
	}
	_, _ = e.WriteString("\x1b[<u")
	if e.KbdExplicit() {
		t.Fatal("second pop should clear the stack")
	}
	// Pop on an empty stack is a no-op, not negative.
	_, _ = e.WriteString("\x1b[<u")
	if e.KbdExplicit() {
		t.Fatal("empty pop changed state")
	}
}

// SPEC G6: with SGR 1016 negotiated, SendMouse writes the supplied
// (pixel) coordinates in the SGR wire format.
func TestSendMousePixelEncoding(t *testing.T) {
	e := NewEmulator(80, 24)
	defer e.Close()
	_, _ = e.WriteString("\x1b[?1002h\x1b[?1016h")
	ch := expectResponse(e)
	e.SendMouse(uv.MouseClickEvent(uv.Mouse{X: 33, Y: 70, Button: uv.MouseLeft}))
	if got, want := waitResponse(t, ch), []byte("\x1b[<0;34;71M"); !bytes.Contains(got, want) {
		t.Fatalf("response = %q, want it to contain %q", got, want)
	}
}

// With only SGR 1006, SendMouse keeps cell coordinates in the same SGR
// wire format.
func TestSendMouseCellEncoding(t *testing.T) {
	e := NewEmulator(80, 24)
	defer e.Close()
	_, _ = e.WriteString("\x1b[?1002h\x1b[?1006h")
	ch := expectResponse(e)
	e.SendMouse(uv.MouseClickEvent(uv.Mouse{X: 3, Y: 7, Button: uv.MouseLeft}))
	if got, want := waitResponse(t, ch), []byte("\x1b[<0;4;8M"); !bytes.Contains(got, want) {
		t.Fatalf("response = %q, want it to contain %q", got, want)
	}
}

// A flags-0 query (our own response echoed back through a canonical tty)
// must not be re-answered: that would loop forever.
func TestFixTermResponseNotReanswered(t *testing.T) {
	e := NewEmulator(80, 24)
	defer e.Close()
	_, _ = e.WriteString("\x1b[?0u")
	select {
	case b := <-expectResponse(e):
		t.Fatalf("response to %q = %q, want silence", "\x1b[?0u", b)
	case <-time.After(150 * time.Millisecond):
	}
}

// A flags query with a nonzero flags field still gets an answer.
func TestFixTermFlagsQuery(t *testing.T) {
	e := NewEmulator(80, 24)
	defer e.Close()
	ch := expectResponse(e)
	_, _ = e.WriteString("\x1b[?1u")
	if got := waitResponse(t, ch); !bytes.Equal(got, []byte("\x1b[?0u")) {
		t.Fatalf("response = %q", got)
	}
}
