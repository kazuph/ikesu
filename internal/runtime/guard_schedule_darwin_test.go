//go:build darwin

package runtime

import (
	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGuardScheduleRepairsWithoutPeriodicTargets(t *testing.T) {
	teststate.Lock(t)
	root := t.TempDir()
	teststate.Setenv(t, "HOME", root)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	s := NewSupervisor()
	defer s.KillAll()
	s.SetGuard(guard.Config{Mode: "record"}, root)
	p, err := s.SpawnWith(1, root, 80, 24, nil, []string{"/bin/sleep", "60"})
	if err != nil {
		t.Fatal(err)
	}
	p.SetAgent("codex")
	wait := func(check func() bool) {
		t.Helper()
		until := time.Now().Add(5 * time.Second)
		for {
			s.PollPendingProcesses(time.Now())
			if check() {
				return
			}
			if time.Now().After(until) {
				t.Fatal("guard schedule did not converge", p.Snapshot())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait(func() bool {
		s.processMu.Lock()
		defer s.processMu.Unlock()
		return !s.processTicks[p] && !s.processDue[p].IsZero() && len(s.processWork) == 0
	})
	for _, path := range []string{p.guard.MarkerPath, filepath.Join(p.guard.Bin, "git")} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		wait(func() bool { _, err := os.Stat(path); return err == nil })
	}
	s.Kill(p.ID)
	s.PollPendingProcesses(time.Now())
	if _, err := os.Stat(p.guard.MarkerPath); !os.IsNotExist(err) {
		t.Fatal("closed guard resurrected", err)
	}
}
