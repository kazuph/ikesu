package server

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/dispatch"
)

func TestG9JobSnapshotAdaptiveIntervalAndExternalWrite(t *testing.T) {
	teststate.Lock(t)
	now := time.Now()
	old := now.Add(-2 * time.Minute).UnixMilli()
	recent := now.UnixMilli()
	for _, tc := range []struct {
		jobs []dispatch.Job
		want time.Duration
	}{
		{nil, 10 * time.Second},
		{[]dispatch.Job{{Status: "exited", FinishedMs: &old}}, 10 * time.Second},
		{[]dispatch.Job{{Status: "exited", FinishedMs: &recent}}, time.Second},
		{[]dispatch.Job{{Status: "queued"}}, time.Second},
		{[]dispatch.Job{{Status: "running"}}, time.Second},
		{[]dispatch.Job{{Status: "cancelling"}}, time.Second},
	} {
		if jobsRefreshInterval(tc.jobs, now) != tc.want {
			t.Fatal(tc)
		}
	}
	s := newTestServer(t, "job-cache")
	store, err := dispatch.Open(dispatch.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s.jobCache.wake = make(chan struct{}, 1)
	s.app.SetJobSource(s.cachedJobSnapshot)
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	stop, done := make(chan struct{}), make(chan struct{})
	go s.jobCacheLoop(stop, done)
	defer func() { close(stop); <-done; s.app.Stop(); <-appDone }()
	readAt := func() time.Time {
		s.jobCache.mu.RLock()
		defer s.jobCache.mu.RUnlock()
		if s.jobCache.err != nil {
			t.Fatal(s.jobCache.err)
		}
		return s.jobCache.readAt
	}
	deadline := time.Now().Add(5 * time.Second)
	for readAt().IsZero() {
		if time.Now().After(deadline) {
			t.Fatal("initial snapshot absent")
		}
		time.Sleep(time.Millisecond)
	}
	initial := readAt()
	time.Sleep(1200 * time.Millisecond)
	if !readAt().Equal(initial) {
		t.Fatal("idle snapshot was re-read after one second")
	}
	// A second real SQLite connection writes while the worker is idle.
	changedAt := time.Now()
	if err := store.InsertCommand("external-job", "external-job", "true", s.cwd, "p1", "owner", "none", ""); err != nil {
		t.Fatal(err)
	}
	deadline = changedAt.Add(time.Second)
	for {
		jobs, _ := s.cachedJobSnapshot()
		if len(jobs) == 1 && jobs[0].ID == "external-job" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("external job write did not refresh within one second", jobs)
		}
		time.Sleep(time.Millisecond)
	}
	beforeWake := readAt()
	s.invalidateJobCache()
	deadline = time.Now().Add(time.Second)
	for readAt().Equal(beforeWake) {
		if time.Now().After(deadline) {
			t.Fatal("run/message invalidation did not refresh immediately")
		}
		time.Sleep(time.Millisecond)
	}
}
