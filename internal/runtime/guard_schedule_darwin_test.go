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

// This test runs longer than five seconds to exercise the real guard deadline.
func TestQuietGuardChecksUnwatchedAncestorWithinDeadline(t *testing.T) {
	teststate.Lock(t)
	base, err := os.MkdirTemp("/tmp", "ikesu-guard-deadline-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(base, 0700); _ = os.RemoveAll(base) })
	root := filepath.Join(base, "parent", "state")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
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
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	setupDeadline := time.NewTimer(5 * time.Second)
	defer setupDeadline.Stop()
	for {
		s.PollPendingProcesses(time.Now())
		s.processMu.Lock()
		quiet := !s.processTicks[p] && !s.processDue[p].IsZero() && len(s.processWork) == 0
		s.processMu.Unlock()
		if quiet && p.guard.ObserveChanges(p.queueProcessChange) {
			break
		}
		select {
		case <-tick.C:
		case <-setupDeadline.C:
			t.Fatal("guarded pane did not become quiet")
		}
	}
	// base is above the watched parent of root; chmod must be found by the deadline.
	if err := os.Chmod(base, 0777); err != nil {
		t.Fatal(err)
	}
	changedAt := time.Now()
	deadline := time.NewTimer(5*time.Second + foregroundJobCacheLifetime)
	defer deadline.Stop()
	for {
		s.PollPendingProcesses(time.Now())
		if issue := p.guard.Issue(); issue != "" {
			elapsed := time.Since(changedAt)
			if elapsed > 5*time.Second+foregroundJobCacheLifetime {
				t.Fatalf("late guard check: %s", elapsed)
			}
			t.Logf("quiet pane detected unmonitored ancestor after %s", elapsed)
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("quiet pane missed ancestor check deadline")
		}
	}
}
