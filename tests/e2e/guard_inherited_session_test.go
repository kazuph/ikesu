package e2e

import (
	"strings"
	"testing"
	"time"
)

func TestServerRemovesInheritedGuardSessionFromPane(t *testing.T) {
	t.Parallel()
	s := startVersionMode(t, map[string]string{"HOME": t.TempDir(), "AI_GUARD_SESSION": "1"}, false, binPath, binPath)
	s.waitFor("prompt", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.send("if [[ -v AI_GUARD_SESSION ]]; then echo GUARD_INHERITED_\"BAD\"; else echo GUARD_INHERITED_\"OK\"; fi\r")
	s.waitFor("guard environment result", 10*time.Second, func(text string) bool {
		return strings.Contains(text, "GUARD_INHERITED_OK") || strings.Contains(text, "GUARD_INHERITED_BAD")
	})
	if strings.Contains(s.text(), "GUARD_INHERITED_BAD") {
		t.Fatal("server passed AI_GUARD_SESSION into the pane")
	}
}
