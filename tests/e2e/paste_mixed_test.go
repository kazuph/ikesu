//go:build pasteversions

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestModalPasteMixedVersionsActualPTY(t *testing.T) {
	old := os.Getenv("PASTE_BASELINE_BIN")
	if old == "" || filepath.Base(old) != "gtx" {
		t.Fatal("PASTE_BASELINE_BIN must be the original binary named gtx")
	}
	for _, pair := range []struct {
		name, client, server string
		supported            bool
	}{
		{"new-client-old-server", binPath, old, false},
		{"old-client-new-server", old, binPath, true},
	} {
		t.Run(pair.name, func(t *testing.T) {
			s := startVersionMode(t, nil, false, pair.client, pair.server)
			s.waitFor("shell", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
			s.send("\x02c")
			s.waitFor("name field", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "new tab") })
			s.send("\x1b[200~SAFE_MODAL_PASTE\x1b[201~")
			if pair.supported {
				s.waitFor("old client paste accepted", 5*time.Second, func(tx string) bool {
					return strings.Contains(tx, "SAFE_MODAL_PASTE") && strings.Contains(tx, "new tab")
				})
				s.send("\r")
				s.waitFor("old client can confirm", 5*time.Second, func(tx string) bool { return hasTabLabel(tx, "SAFE_MODAL_PASTE") && !strings.Contains(tx, "new tab") })
			} else {
				s.waitFor("unsupported paste explained", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "endpoint_action_unsupported: paste") })
				if strings.Contains(s.text(), "SAFE_MODAL_PASTE") || !strings.Contains(s.text(), "new tab") {
					t.Fatal("unsupported payload changed field or pane", s.text())
				}
				s.send("\x1b[27u")
				s.waitFor("cancel unsupported field", 5*time.Second, func(tx string) bool { return !strings.Contains(tx, "new tab") })
			}
			s.send("printf 'MIX_%s\\n' PASTE_READY\r")
			s.waitFor("ordinary pane input retained", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "MIX_PASTE_READY") })
			t.Logf("actual outer PTY: %s, clipboard body contained=%t, ordinary pane input preserved", pair.name, pair.supported)
		})
	}
}
