package runtime

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/teststate"
)

// The queue holds exactly one full read. Back-to-back full 64KB chunks must
// each be released before the next is queued, and EOF must still end the
// pump after the last byte reaches the emulator.
func TestPumpFullChunksThenEOFDeliverEverything(t *testing.T) {
	teststate.Lock(t)

	p, w, evCh := newPumpPane(t)
	p.startPumpWithTiming(pumpBatchWait, pumpStreamGap)
	go func() {
		for range evCh {
			p.MarkChangedRead()
		}
	}()

	var b strings.Builder
	for i := 0; b.Len() < 6*pumpReadBytes; i++ {
		fmt.Fprintf(&b, "row %06d %s\r\n", i, strings.Repeat("x", 60))
	}
	b.WriteString("TAIL-MARK")
	payload := b.String()
	writeErr := make(chan error, 1)
	go func() {
		// Writes in full read-sized pieces so the reader sees full chunks.
		for off := 0; off < len(payload); off += pumpReadBytes {
			if _, err := w.WriteString(payload[off:min(len(payload), off+pumpReadBytes)]); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- w.Close()
	}()

	select {
	case err := <-writeErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("writer blocked: the reader stopped draining full chunks")
	}
	select {
	case <-p.pumpDone:
	case <-time.After(5 * time.Second):
		t.Fatal("pump did not finish after EOF")
	}
	if s := screenText(p); !strings.Contains(s, "TAIL-MARK") {
		t.Fatalf("last bytes missing after EOF: %q", s[max(0, len(s)-200):])
	}
}
