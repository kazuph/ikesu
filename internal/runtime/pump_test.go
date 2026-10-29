package runtime

import (
	"github.com/kazuph/ikesu/internal/teststate"
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
	p := &Pane{ID: 1, emu: emu, ev: evCh}
	if err := p.installPTY(r); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = w.Close()
		if p.pumpDone != nil {
			<-p.pumpDone
		}
		p.closePTY()
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
	teststate.Lock(t)
	const batchWait = 200 * time.Millisecond // large: a wrongly-always-batched impl is obvious

	p, w, _ := newPumpPane(t)
	p.startPumpWithTiming(batchWait, 5*time.Millisecond)
	want := ""
	for _, letter := range []string{"x", "y"} {
		time.Sleep(50 * time.Millisecond) // idle, including after an earlier burst
		want += letter
		t0 := time.Now()
		if _, err := w.WriteString(letter); err != nil {
			t.Fatal(err)
		}
		deadline := t0.Add(50 * time.Millisecond) // far below pumpBatchWait
		seen := false
		for time.Now().Before(deadline) {
			if s := screenText(p); len(s) >= len(want) && s[:len(want)] == want {
				seen = true
				break
			}
			time.Sleep(200 * time.Microsecond)
		}
		if !seen {
			t.Fatalf("first byte after idle did not reach emulator within %v (batch wait %v)", time.Since(t0), batchWait)
		}
	}
}

// A previous pane can still drain output after its supervisor is killed.
// Starting a pump with test timings must not race with that live reader.
func TestPumpTimingsAreIndependentOfLivePane(t *testing.T) {
	p, w, _ := newPumpPane(t)
	p.startPump()
	started, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		_, err := w.WriteString("x")
		close(started)
		for err == nil {
			<-tick.C
			_, err = w.WriteString("x")
		}
	}()
	t.Cleanup(func() {
		_ = w.Close()
		<-done
	})
	<-started
	t.Run("idle-pane", TestPumpFirstOutputAfterIdleIsImmediate)
}

// TestPumpStreamingCoalesces verifies reads are gathered while output keeps
// flowing: bytes arriving over a window much shorter than pumpBatchWait land
// through a small number of batches (few change notifications), and every
// byte — including the last — still reaches the screen.
func TestPumpStreamingCoalesces(t *testing.T) {
	teststate.Lock(t)

	p, w, evCh := newPumpPane(t)
	p.startPumpWithTiming(40*time.Millisecond, 200*time.Millisecond)

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
	teststate.Lock(t)

	p, w, evCh := newPumpPane(t)
	p.startPumpWithTiming(40*time.Millisecond, 200*time.Millisecond)
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
