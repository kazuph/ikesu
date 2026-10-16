//go:build stage1legacy

package e2e

import (
	"os"
	"testing"
)

func TestOldServerLiveKeyboardHandoff(t *testing.T) {
	old := os.Getenv("STAGE1_OLD_BIN")
	if old == "" {
		t.Fatal("STAGE1_OLD_BIN must name the unchanged pre-keyboard-state server")
	}
	liveKeyboardHandoff(t, old)
}
