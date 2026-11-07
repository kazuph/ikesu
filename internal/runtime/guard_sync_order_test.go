package runtime

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/platform"
)

// The process worker can apply "no agent" while an explicit report sets an
// agent on the app loop. Whichever Sync runs last must use the latest agent,
// so the explicit report's guard is not removed by the older observation.
func TestGuardSyncAfterObservationKeepsExplicitAgent(t *testing.T) {
	root := t.TempDir()
	a := guard.NewAssets(root, "p_1", "/usr/bin/true", guard.Config{})
	if _, err := a.Prepare([]string{"HOME=" + root, "PATH=/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + filepath.Join(root, "s.sock")}, root); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync("claude"); err != nil {
		t.Fatal(err)
	}
	p := &Pane{ID: 1, ev: make(chan Event, 8), guard: a, agentID: "claude", procName: "claude", fgPID: 100}

	// Hold the guard ordering lock so the observation stops right before Sync.
	p.guardSyncMu.Lock()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.applyProcessObservation(processObservation{
			seq:  1,
			job:  platform.ForegroundJob{ProcessGroupID: 200, Processes: []platform.ForegroundProcess{{PID: 200, Name: "zsh"}}},
			name: "zsh",
		})
	}()
	waitPane(t, p, func() bool { return p.fgPID == 200 })
	// The explicit report lands and finishes its guard update while the
	// observation waits; SetAgent itself would wait on the held lock, so apply
	// its two steps here in the same order.
	p.mu.Lock()
	p.agentOverride = "codex"
	p.mu.Unlock()
	if err := a.Sync("codex"); err != nil {
		t.Fatal(err)
	}
	p.guardSyncMu.Unlock()
	wg.Wait()

	meta, active, err := guard.ReadMarker(guard.Input{Env: map[string]string{
		"IKESU_GUARD_MARKER": a.MarkerPath, "IKESU_PANE_ID": "p1", "IKESU_SOCKET_PATH": filepath.Join(root, "s.sock"),
	}})
	if err != nil || !active || meta.Agent != "codex" {
		t.Fatalf("explicit agent guard lost: active=%v agent=%q err=%v", active, meta.Agent, err)
	}
}

func waitPane(t *testing.T, p *Pane, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		done := ok()
		p.mu.Unlock()
		if done {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pane state not reached")
}
