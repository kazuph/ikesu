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
	switch {
	case seconds >= day:
		return fmt.Sprintf("%dd", seconds/day)
	case seconds >= hour:
		return fmt.Sprintf("%dh", seconds/hour)
	case seconds >= minute:
		return fmt.Sprintf("%dm", seconds/minute)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}
