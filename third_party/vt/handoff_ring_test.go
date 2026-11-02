package vt

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// A byte-limited history allows up to limit/24 rows, but an imported terminal
// must not reserve that many empty ring slots up front: a live handoff of a
// 10MB budget used to pin about 27MB of empty slots per pane.
func TestHandoffDoesNotReserveByteLimitedRingUpFront(t *testing.T) {
	const limit = 10_000_000
	a, b := NewEmulator(40, 4), NewEmulator(1, 1)
	defer a.Close()
	defer b.Close()
	a.SetScrollbackSize(1000)
	a.Scrollback().SetMaxBytes(limit)
	for i := 0; i < 30; i++ {
		_, _ = a.WriteString(fmt.Sprintf("\x1b[3%dmline-%02d\x1b[m\r\n", i%7, i))
	}
	state, err := a.MarshalHandoff()
	if err != nil {
		t.Fatal(err)
	}
	if err = b.UnmarshalHandoff(state); err != nil {
		t.Fatal(err)
	}
	src, dst := a.Scrollback(), b.Scrollback()
	if got := len(dst.lines); got > 2*max(src.Len(), 1) {
		t.Fatalf("imported ring reserved %d slots for %d rows", got, src.Len())
	}
	if len(dst.rowBytes) != len(dst.lines) || len(dst.packed) != len(dst.lines) || len(dst.styled) != len(dst.lines) {
		t.Fatalf("byte ring arrays disagree: lines=%d sizes=%d packed=%d styled=%d", len(dst.lines), len(dst.rowBytes), len(dst.packed), len(dst.styled))
	}
	if dst.maxLines != src.maxLines || dst.maxBytes != src.maxBytes || !dst.byteLimited {
		t.Fatalf("limits changed: lines %d->%d bytes %d->%d", src.maxLines, dst.maxLines, src.maxBytes, dst.maxBytes)
	}
	if !reflect.DeepEqual(src.Lines(), dst.Lines()) || src.Bytes() != dst.Bytes() {
		t.Fatal("imported history differs")
	}
	// Growing past the imported size behaves as in the original terminal.
	for _, e := range []*Emulator{a, b} {
		for i := 0; i < 3000; i++ {
			_, _ = e.WriteString(fmt.Sprintf("\x1b[3%dmmore-%04d\x1b[m\r\n", i%7, i))
		}
	}
	if !reflect.DeepEqual(src.Lines(), dst.Lines()) || src.Bytes() != dst.Bytes() || src.Len() != dst.Len() {
		t.Fatalf("history after growth differs: len %d vs %d", src.Len(), dst.Len())
	}
}

// After import, a small budget must evict and wrap the grown ring exactly as
// the original terminal does, including from an empty imported history.
func TestHandoffImportedRingEvictsLikeOriginal(t *testing.T) {
	for _, before := range []int{0, 1, 30} {
		t.Run(fmt.Sprint(before), func(t *testing.T) {
			a, b := NewEmulator(40, 4), NewEmulator(1, 1)
			defer a.Close()
			defer b.Close()
			a.SetScrollbackSize(1000)
			a.Scrollback().SetMaxBytes(60_000) // roughly 13 rows of 40 cells
			for i := 0; i < before; i++ {
				_, _ = a.WriteString(fmt.Sprintf("\x1b[3%dmold-%02d\x1b[m\r\n", i%7, i))
			}
			state, err := a.MarshalHandoff()
			if err != nil {
				t.Fatal(err)
			}
			if err = b.UnmarshalHandoff(state); err != nil {
				t.Fatal(err)
			}
			src, dst := a.Scrollback(), b.Scrollback()
			if src.Len() != dst.Len() || dst.maxBytes != src.maxBytes || dst.maxLines != src.maxLines {
				t.Fatalf("import differs: len %d/%d", src.Len(), dst.Len())
			}
			for i := 0; i < 200; i++ {
				line := fmt.Sprintf("\x1b[3%dmnew-%03d %s\x1b[m\r\n", i%7, i, strings.Repeat("x", i%30))
				for _, e := range []*Emulator{a, b} {
					_, _ = e.WriteString(line)
				}
				if !reflect.DeepEqual(src.Lines(), dst.Lines()) || src.Bytes() != dst.Bytes() {
					t.Fatalf("after %d rows: history differs (len %d vs %d)", i+1, src.Len(), dst.Len())
				}
			}
			if dst.Bytes() > dst.maxBytes || dst.Len() >= 200 {
				t.Fatalf("budget not enforced: bytes %d limit %d rows %d", dst.Bytes(), dst.maxBytes, dst.Len())
			}
		})
	}
}
