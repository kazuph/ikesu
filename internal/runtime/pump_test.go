package runtime

import (
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// newPumpPane builds a Pane whose "pty" is an os.Pipe so the pump loop can
// be driven deterministically from the test. It returns the pane, the
// writable end, and the pane's event channel.
func newPumpPane(t *testing.T) (*Pane, *os.File, chan Event) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	emu := vt.NewEmulator(80, 24)
	go io.Copy(io.Discard, emu)
	evCh := make(chan Event, 64)
	p := &Pane{ID: 1, ptyFile: r, emu: emu, ev: evCh}
	t.Cleanup(func() {
		_ = w.Close()
		_ = r.Close()
	})
	return p, w, evCh
}

func screenText(p *Pane) string {
	var s string
	p.WithEmu(func(e *vt.Emulator) { s = e.String() })
	return s
}

// TestPumpFirstOutputAfterIdleIsImmediate writes a single byte after an idle
// gap and checks it lands in the emulator long before pumpBatchWait could
// have delayed it — the batching wait must not apply to the first read.
func TestPumpFirstOutputAfterIdleIsImmediate(t *testing.T) {
	oldWait, oldGap := pumpBatchWait, pumpStreamGap
	pumpBatchWait = 200 * time.Millisecond // large: a wrongly-always-batched impl is obvious
	pumpStreamGap = 5 * time.Millisecond
	defer func() { pumpBatchWait, pumpStreamGap = oldWait, oldGap }()

	p, w, _ := newPumpPane(t)
	go p.pumpPTYToEmu()
	time.Sleep(50 * time.Millisecond) // pump settles into the blocking read

	t0 := time.Now()
	if _, err := w.WriteString("x"); err != nil {
		t.Fatal(err)
	}
	deadline := t0.Add(50 * time.Millisecond) // far below pumpBatchWait
	for time.Now().Before(deadline) {
		if s := screenText(p); len(s) > 0 && s[0] == 'x' {
			return
		}
		time.Sleep(200 * time.Microsecond)
	}
	t.Fatalf("first byte after idle did not reach emulator within %v (batch wait %v)", time.Since(t0), pumpBatchWait)
}

// TestPumpStreamingCoalesces verifies reads are gathered while output keeps
// flowing: bytes arriving over a window much shorter than pumpBatchWait land
// through a small number of batches (few change notifications), and every
// byte — including the last — still reaches the screen.
func TestPumpStreamingCoalesces(t *testing.T) {
	oldWait, oldGap := pumpBatchWait, pumpStreamGap
	pumpBatchWait = 40 * time.Millisecond
	pumpStreamGap = 200 * time.Millisecond
	defer func() { pumpBatchWait, pumpStreamGap = oldWait, oldGap }()

	p, w, evCh := newPumpPane(t)
	go p.pumpPTYToEmu()

	var events atomic.Int32
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case ev := <-evCh:
				if ev.Kind == EventChanged {
					p.MarkChangedRead()
					events.Add(1)
				}
			case <-stop:
				return
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	const letters = "abcdefghijklmnopqrst"
	// Spaced writes: without gathering each byte would be its own read and
	// notification; with the ~40ms gather the whole tail merges into one or
	// two batches.
	for i := 0; i < len(letters); i++ {
		if _, err := w.WriteString(letters[i : i+1]); err != nil {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Microsecond)
	}

	deadline := time.Now().Add(time.Second)
	var s string
	for time.Now().Before(deadline) {
		s = screenText(p)
		if len(s) >= len(letters) && s[:len(letters)] == letters {
			break
		}
		time.Sleep(500 * time.Microsecond)
	}
	if len(s) < len(letters) || s[:len(letters)] != letters {
		t.Fatalf("final output missing: screen=%q want %q", s, letters)
	}
	if got := events.Load(); got > 6 {
		t.Fatalf("burst produced %d change notifications; batching should keep it near 2", got)
	}
}

// TestPumpDeliversTrailingOutputAfterQuiet covers the stream-tail case: the
// very last bytes of a burst must still be flushed even though nothing
// follows them.
func TestPumpDeliversTrailingOutputAfterQuiet(t *testing.T) {
	oldWait, oldGap := pumpBatchWait, pumpStreamGap
	pumpBatchWait = 40 * time.Millisecond
	pumpStreamGap = 200 * time.Millisecond
	defer func() { pumpBatchWait, pumpStreamGap = oldWait, oldGap }()

	p, w, evCh := newPumpPane(t)
	go p.pumpPTYToEmu()
	go func() {
		for range evCh {
			p.MarkChangedRead()
		}
	}()

	time.Sleep(30 * time.Millisecond)
	if _, err := w.WriteString("start"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := w.WriteString("END"); err != nil { // the final bytes; nothing follows
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s := screenText(p); len(s) >= 8 && s[:8] == "startEND" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("trailing bytes never reached emulator: screen=%q", screenText(p))
}
