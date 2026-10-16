package tui

import (
	"fmt"
	"time"
)

func formatJobAge(elapsedMs int64) string {
	seconds := max(int64(0), elapsedMs) / int64(time.Second/time.Millisecond)
	const minute = int64(time.Minute / time.Second)
	const hour = int64(time.Hour / time.Second)
	const day = int64(24 * time.Hour / time.Second)
	unit := "s"
	switch {
	case seconds >= day:
		seconds, unit = seconds/day, "d"
	case seconds >= hour:
		seconds, unit = seconds/hour, "h"
	case seconds >= minute:
		seconds, unit = seconds/minute, "m"
	}
	// The job column is three right-aligned digits followed by one unit.
	return fmt.Sprintf("%3d%s", min(seconds, int64(999)), unit)
}
