package server

import (
	"testing"
	"time"
)

func waitPromptCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("PTY condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}
