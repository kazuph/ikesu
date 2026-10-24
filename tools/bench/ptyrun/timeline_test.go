package main

import (
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/vt"
)

func TestTimelineWaitsForCompleteSynchronizedFrame(t *testing.T) {
	emu := vt.NewEmulator(20, 8)
	observer := newTimeline(filepath.Join(t.TempDir(), "frames.jsonl"), emu, 20, 8)
	defer observer.close()
	observer.feed([]byte("\x1b[?2026h\x1b[2;2H┌─────────────┐\x1b[6;2H└─────────────┘\x1b[3;3H@100010c03!\x1b[?202"), 100)
	if len(observer.latest.Panes) != 0 {
		t.Fatal("incomplete frame was observed")
	}
	observer.feed([]byte("6l"), 200)
	if observer.latest.At != 200 || len(observer.latest.Panes) != 1 {
		t.Fatalf("missing completed frame: %+v", observer.latest)
	}
	pane := observer.latest.Panes[0]
	if pane.ID != 1 || pane.Seq != 1 || pane.Width != 12 || pane.Height != 3 || pane.LayoutWidth != 12 || pane.LayoutHeight != 3 {
		t.Fatalf("wrong dimensions/sequence: %+v", pane)
	}
}
func TestMonotonicClockNeverGoesBackwards(t *testing.T) {
	before := monotonicNS()
	for range 1000 {
		after := monotonicNS()
		if after < before {
			t.Fatalf("clock regressed: %d -> %d", before, after)
		}
		before = after
	}
}
