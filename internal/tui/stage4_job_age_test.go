package tui

import (
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/dispatch"
	"github.com/kazuph/goterrace/internal/render"
)

func TestStage4JobAgeUnitsAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{
		{-time.Second, "  0s"},
		{0, "  0s"},
		{time.Minute - time.Millisecond, " 59s"},
		{time.Minute, "  1m"},
		{time.Minute + time.Second, "  1m"},
		{time.Hour - time.Millisecond, " 59m"},
		{time.Hour, "  1h"},
		{24*time.Hour - time.Millisecond, " 23h"},
		{24 * time.Hour, "  1d"},
		{100 * 24 * time.Hour, "100d"},
		{999 * 24 * time.Hour, "999d"},
		{1000 * 24 * time.Hour, "999d"},
	} {
		if got := formatJobAge(tc.age.Milliseconds()); got != tc.want {
			t.Fatalf("age %s: got %q want %q", tc.age, got, tc.want)
		}
	}
}

func TestStage4JobAgeAdvancesWithoutAnotherSnapshot(t *testing.T) {
	now := time.Unix(1, 0)
	since := now.Add(-time.Minute + time.Second).UnixMilli()
	a := &App{jobRows: []render.JobRow{{AgeSinceMs: &since, Elapsed: " 59s"}}}
	a.refreshJobDots(now.Add(time.Second))
	if a.jobRows[0].Elapsed != "  1m" || !a.dirty {
		t.Fatalf("minute boundary did not refresh the row: %+v", a.jobRows[0])
	}
}

// refreshJobDots accepts the clock value; no persisted timestamp is rewritten.
func TestStage4JobAgeUsesStatusClockAndMissingTime(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	started := now.Add(-25 * time.Hour).UnixMilli()
	finished := now.Add(-23 * time.Hour).UnixMilli()
	for _, tc := range []struct {
		name, status  string
		start, finish *int64
		want          string
	}{
		{"running uses start", "running", &started, &finished, "  1d"},
		{"finished uses finish", "finished", &started, &finished, " 23h"},
		{"finished without start", "finished", nil, &finished, " 23h"},
		{"running without start", "running", nil, &finished, "   -"},
		{"finished without finish", "finished", &started, nil, "   -"},
		{"cancelling", "cancelling", &started, &finished, "   -"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{jobSource: func() ([]dispatch.Job, map[string]bool) {
				return []dispatch.Job{{ID: "age-job", Status: tc.status, StartedMs: tc.start, FinishedMs: tc.finish}}, nil
			}}
			a.refreshJobDots(now)
			if len(a.jobRows) != 1 || a.jobRows[0].Elapsed != tc.want {
				t.Fatalf("rows: %+v want %q", a.jobRows, tc.want)
			}
		})
	}
}
