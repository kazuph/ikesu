package runtime

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/teststate"
	"golang.org/x/sync/semaphore"
)

// The queue holds exactly one full read. Output written in read-sized pieces
// (reads may still split them) must keep flowing through the one-chunk
// budget, and EOF must still end the pump after the last byte reaches the
// emulator.
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
		// Read-sized writes; the kernel may split them into smaller reads.
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

// Small reads are copied into exact-size chunks so many queued small chunks
// do not each pin a full read buffer; large reads hand the buffer over.
func TestReadPTYCopiesSmallReadsAndHandsOverLargeOnes(t *testing.T) {
	p, w, _ := newPumpPane(t)
	out := make(chan []byte, pumpQueueLen)
	pool := make(chan []byte, pumpPoolLen)
	sem := semaphore.NewWeighted(pumpQueueBytes)
	go p.readPTY(out, pool, sem)

	next := func() []byte {
		t.Helper()
		select {
		case c := <-out:
			sem.Release(int64(len(c)))
			return c
		case <-time.After(5 * time.Second):
			t.Fatal("no chunk from the reader")
			return nil
		}
	}
	if _, err := w.WriteString("small"); err != nil {
		t.Fatal(err)
	}
	if c := next(); string(c) != "small" || cap(c) > pumpSmallRead {
		t.Fatalf("small read: %q cap %d", c, cap(c))
	}
	big := strings.Repeat("b", pumpSmallRead*2)
	if _, err := w.WriteString(big); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	sawFull := false
	for got.Len() < len(big) {
		c := next()
		got.Write(c)
		if len(c) > pumpSmallRead && cap(c) == pumpReadBytes {
			sawFull = true
		}
	}
	if got.String() != big || !sawFull {
		t.Fatalf("large read: %d bytes, full buffer handed over=%v", got.Len(), sawFull)
	}
}
