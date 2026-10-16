package tui

import (
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/render"
)

func TestStage4JobAgeUnitsAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{
		{-time.Second, "0s"},
		{0, "0s"},
		{time.Minute - time.Millisecond, "59s"},
		{time.Minute, "1m"},
		{time.Minute + time.Second, "1m"},
		{time.Hour - time.Millisecond, "59m"},
		{time.Hour, "1h"},
		{24*time.Hour - time.Millisecond, "23h"},
		{24 * time.Hour, "1d"},
	} {
		if got := formatJobAge(tc.age.Milliseconds()); got != tc.want {
			t.Fatalf("age %s: got %q want %q", tc.age, got, tc.want)
		}
	}
}

func TestStage4JobAgeAdvancesWithoutAnotherSnapshot(t *testing.T) {
	now := time.Unix(1, 0)
	since := now.Add(-time.Minute + time.Second).UnixMilli()
	a := &App{jobRows: []render.JobRow{{AgeSinceMs: &since, Elapsed: "59s"}}}
	a.refreshJobDots(now.Add(time.Second))
	if a.jobRows[0].Elapsed != "1m" || !a.dirty {
		t.Fatalf("minute boundary did not refresh the row: %+v", a.jobRows[0])
	}
}
