package e2e

import (
	"strings"
	"testing"
	"time"
)

// Starting a real terminal and transferring the server can exceed five seconds.
func TestOyagoiSurvivesLiveHandoff(t *testing.T) {
	s := start(t)
	s.waitFor("initial shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	if out, err := s.cli("workspace", "rename", "s1", "HQ-handoff"); err != nil {
		t.Fatalf("rename workspace: %s %v", out, err)
	}
	s.waitFor("renamed workspace", 15*time.Second, func(text string) bool { return strings.Contains(text, "HQ-handoff") })
	x, y := s.find("HQ-handoff")
	s.click(x, y)
	s.waitFor("HQ menu", 15*time.Second, func(text string) bool { return strings.Contains(text, "set as HQ") })
	x, y = s.find("set as HQ")
	s.click(x, y)
	s.waitFor("HQ selected", 15*time.Second, func(text string) bool {
		return strings.Contains(text, "🎏 oyagoi") && strings.Contains(text, "HQ-handoff")
	})
	if out, err := s.cli("server", "live-handoff"); err != nil {
		t.Fatalf("live handoff: %s %v", out, err)
	}
	s.send("printf 'HQ_HANDOFF_READY\\n'\r")
	s.waitFor("HQ after handoff", 15*time.Second, func(text string) bool {
		return strings.Contains(text, "HQ_HANDOFF_READY") && strings.Contains(text, "🎏 oyagoi") && strings.Count(text, "HQ-handoff") == 1
	})
}
