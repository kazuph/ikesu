package runtime

import (
	"strings"
	"testing"
)

func TestDetectionLinesUnwrapScreenAnd24HistoryRows(t *testing.T) {
	s := NewSupervisor()
	p, err := s.SpawnWith(1, t.TempDir(), 8, 4, nil, []string{"/bin/sh", "-c", "printf 'HISTORY\\nabc def ghi\\nEND\\n'; exec /bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.KillAll()
	waitReadMarker(t, p, "END")
	got := strings.Join(p.DetectionLines(), "\n")
	if !strings.Contains(got, "abc def ghi") || !strings.Contains(got, "HISTORY") {
		t.Fatalf("detection=%q", got)
	}
	history, _ := p.ReadLines("recent_unwrapped", true)
	if !strings.Contains(strings.Join(history, "\n"), "HISTORY") {
		t.Fatal("history setup did not scroll")
	}
}

func TestDetectionHistoryIsBoundedAndIndependentOfVisibleScreen(t *testing.T) {
	s := NewSupervisor()
	p, err := s.SpawnWith(1, t.TempDir(), 80, 4, nil, []string{"/bin/sh", "-c", "printf 'OLD_MARKER\\n'; i=0; while [ $i -lt 40 ]; do printf 'row-%s\\n' $i; i=$((i+1)); done; printf 'END\\n'; exec /bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.KillAll()
	waitReadMarker(t, p, "END")
	expanded := strings.Join(p.DetectionLines(), "\n")
	visible := strings.Join(p.DetectionScreenLines(), "\n")
	if strings.Contains(expanded, "OLD_MARKER") || !strings.Contains(expanded, "row-20") || strings.Contains(visible, "row-20") {
		t.Fatal(expanded, visible)
	}
	if len(p.DetectionLines()) > 28 {
		t.Fatal("more than 24 history rows", len(p.DetectionLines()))
	}
}
