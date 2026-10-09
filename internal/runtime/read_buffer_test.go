package runtime

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRecordedLogicalRowsAndRawStylesFromRealPTY(t *testing.T) {
	s := NewSupervisor()
	p, err := s.SpawnWith(1, t.TempDir(), 8, 3, nil, []string{"/bin/sh", "-c", "printf '\033[31mabcdefgh ijklmnop\033[0m\\nHARD\\nEND\\n'; exec /bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.KillAll()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		lines, _ := p.ReadLines("recent_unwrapped", true)
		if strings.Contains(strings.Join(lines, "\n"), "END") {
			if strings.Join(lines, "\n") != "abcdefgh ijklmnop\nHARD\nEND\n" {
				t.Fatalf("logical %q", lines)
			}
			raw, _ := p.ReadLines("recent_unwrapped", false)
			if !strings.Contains(strings.Join(raw, "\n"), "\x1b[31mabcdefgh") || !strings.Contains(strings.Join(raw, "\n"), "\x1b[m") && !strings.Contains(strings.Join(raw, "\n"), "\x1b[0m") {
				t.Fatalf("raw lost ANSI: %q", raw)
			}
			break
		}
		select {
		case <-deadline.C:
			t.Fatalf("output unavailable: %q", lines)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestRecordedRowsRetainSpacesAndBoundedHistoryFromRealPTY(t *testing.T) {
	s := NewSupervisor()
	script := fmt.Sprintf("stty -echo; printf 'abcd    efgh\\n界界abcdef\\nREADY\\n'; read gate; i=0; while [ $i -lt %d ]; do printf 'row-%%04d\\n' $i; i=$((i+1)); done; printf 'TAIL\\n'; read gate; printf '\\033[?1049hALT\\n'; read gate; printf '\\033[?1049lRESTORED\\n'; exec /bin/cat", ScrollbackSize+20)
	p, err := s.SpawnWith(1, t.TempDir(), 8, 2, nil, []string{"/bin/sh", "-c", script})
	if err != nil {
		t.Fatal(err)
	}
	defer s.KillAll()
	lines := waitReadMarker(t, p, "READY")
	if !strings.Contains(strings.Join(lines, "\n"), "abcd    efgh") {
		t.Fatalf("wrap spaces lost: %q", lines)
	}
	if _, err := p.WriteToPTY([]byte("next\n")); err != nil {
		t.Fatal(err)
	}
	lines = waitReadMarker(t, p, "TAIL")
	p.mu.Lock()
	retained := len(p.read.history)
	p.mu.Unlock()
	if retained != ScrollbackSize {
		t.Fatalf("history metadata grew: %d", retained)
	}
	if len(lines) > ScrollbackSize+2 || !strings.Contains(strings.Join(lines, "\n"), "row-1019") {
		t.Fatalf("retained tail wrong: %d", len(lines))
	}
	if _, err := p.WriteToPTY([]byte("next\n")); err != nil {
		t.Fatal(err)
	}
	lines = waitReadMarker(t, p, "ALT")
	if strings.Contains(strings.Join(lines, "\n"), "row-") {
		t.Fatal("main history leaked into alternate screen")
	}
	if _, err := p.WriteToPTY([]byte("next\n")); err != nil {
		t.Fatal(err)
	}
	waitReadMarker(t, p, "RESTORED")
	s.Resize(1, 10, 4)
	lines, _ = p.ReadLines("recent_unwrapped", true)
	if !strings.Contains(strings.Join(lines, "\n"), "row-1019") {
		t.Fatal("resize lost retained history")
	}
}
func waitReadMarker(t *testing.T, p *Pane, marker string) []string {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		lines, err := p.ReadLines("recent_unwrapped", true)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(lines, "\n"), marker) {
			return lines
		}
		select {
		case <-deadline.C:
			t.Fatalf("output unavailable: %q", lines)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestRecordedWideMarginMatchesRenderedCellsFromRealPTY(t *testing.T) {
	s := NewSupervisor()
	p, err := s.SpawnWith(1, t.TempDir(), 8, 4, nil, []string{"/bin/sh", "-c", "printf '1234567界Z\\nWIDE_READY\\n'; exec /bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.KillAll()
	lines := waitReadMarker(t, p, "WIDE_READY")
	physicalRows, err := p.ReadLines("recent", true)
	if err != nil {
		t.Fatal(err)
	}
	physical := strings.Join(physicalRows, "\n")
	logical := strings.Join(lines, "\n")
	if !strings.Contains(physical, "1234567\nZ\nWIDE_REA\nDY") || !strings.Contains(logical, "1234567 Z\nWIDE_READY") {
		t.Fatalf("logical read differs from rendered wide-margin cells: screen=%q logical=%q", physical, logical)
	}
}
