package runtime

import (
	"testing"

	"github.com/kazuph/ikesu/internal/platform"
)

// A scan started earlier must not overwrite a newer scan that was applied
// first: the process worker and API paths observe the same pane concurrently.
func TestProcessObservationOlderScanDoesNotOverwriteNewer(t *testing.T) {
	p := &Pane{ID: 1, ev: make(chan Event, 8), dead: true}
	scan := func(seq uint64, group int, name, cwd string) processObservation {
		return processObservation{
			seq:  seq,
			job:  platform.ForegroundJob{ProcessGroupID: group, Processes: []platform.ForegroundProcess{{PID: group, Name: name}}},
			name: name,
			cwd:  cwd,
		}
	}
	older := scan(1, 100, "old-program", "/old")
	newer := scan(2, 200, "new-program", "/new")

	p.applyProcessObservation(newer)
	p.applyProcessObservation(older)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fgPID != 200 || p.procName != "new-program" || p.procCwd != "/new" {
		t.Fatalf("older scan overwrote newer: fgPID=%d name=%q cwd=%q", p.fgPID, p.procName, p.procCwd)
	}
}

// Scans applied in order still advance the state.
func TestProcessObservationAppliesInScanOrder(t *testing.T) {
	p := &Pane{ID: 1, ev: make(chan Event, 8), dead: true}
	for i, name := range []string{"first", "second"} {
		group := 100 + i
		p.applyProcessObservation(processObservation{
			seq:  uint64(i + 1),
			job:  platform.ForegroundJob{ProcessGroupID: group, Processes: []platform.ForegroundProcess{{PID: group, Name: name}}},
			name: name,
		})
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fgPID != 101 || p.procName != "second" {
		t.Fatalf("in-order scans not applied: fgPID=%d name=%q", p.fgPID, p.procName)
	}
}
