package runtime

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/teststate"
)

func TestResizeKeepsOutputMeasuredScrollbarAndRetainsReadHistoryRealPTY(t *testing.T) {
	teststate.Lock(t)
	for _, preceding := range []int{0, 40} {
		t.Run(fmt.Sprint(preceding), func(t *testing.T) {
			s := NewSupervisor()
			defer s.KillAll()
			command := fmt.Sprintf("i=0; while [ $i -lt %d ]; do printf 'PRE\\n'; i=$((i+1)); done; printf 'A%%0298dZ\\nREADY' 0; exec /bin/cat", preceding)
			p, err := s.SpawnWith(1, t.TempDir(), 80, 12, nil, []string{"/bin/sh", "-c", command})
			if err != nil {
				t.Fatal(err)
			}
			waitReadMarker(t, p, "READY")
			before := p.Snapshot()
			if before.ScrollbarVisible != (before.ScrollbackLen > 0) {
				t.Fatal("PTY output did not measure its history", before)
			}
			s.Resize(1, 80, 3)
			after := p.Snapshot()
			if after.ScrollbackLen <= before.ScrollbackLen || after.ScrollbarVisible != before.ScrollbarVisible {
				t.Fatal("resize must preserve actual rows without changing the output-measured scrollbar", before, after)
			}
			lines, err := p.ReadLines("recent_unwrapped", true)
			if err != nil || !strings.Contains(strings.Join(lines, "\n"), "A"+strings.Repeat("0", 298)+"Z") {
				t.Fatal("display metadata changed the retained read text", lines, err)
			}
			s.Input(1, []byte("\n"))
			deadline := time.Now().Add(3 * time.Second)
			for p.Snapshot().Revision == after.Revision {
				if time.Now().After(deadline) {
					t.Fatal("real PTY did not echo new output")
				}
				time.Sleep(10 * time.Millisecond)
			}
			updated := p.Snapshot()
			if !updated.ScrollbarVisible || updated.ScrollbackLen <= before.ScrollbackLen {
				t.Fatal("new output did not refresh the actual scrollbar", updated)
			}
		})
	}
}
