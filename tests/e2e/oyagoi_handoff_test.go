package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
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

// Starting a real terminal and duplicating its workspace can exceed five seconds.
func TestOyagoiRemainsSingleAfterDuplicate(t *testing.T) {
	s := start(t)
	s.waitFor("initial shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	if out, err := s.cli("workspace", "rename", "s1", "HQ-original"); err != nil {
		t.Fatalf("rename workspace: %s %v", out, err)
	}
	s.waitFor("renamed workspace", 15*time.Second, func(text string) bool { return strings.Contains(text, "HQ-original") })
	x, y := s.find("HQ-original")
	s.click(x, y)
	s.waitFor("HQ menu", 15*time.Second, func(text string) bool { return strings.Contains(text, "set as HQ") })
	x, y = s.find("set as HQ")
	s.click(x, y)
	s.waitFor("HQ selected", 15*time.Second, func(text string) bool {
		return strings.Contains(text, "🎏 oyagoi") && strings.Contains(text, "HQ-original")
	})
	var duplicate api.WorkspaceCreatedResult
	s.rpc(api.MethodWorkspaceDuplicate, map[string]any{"workspace_id": "s1", "focus": false}, &duplicate)
	if duplicate.Workspace.WorkspaceID == "s1" || duplicate.Workspace.WorkspaceID == "" {
		t.Fatalf("duplicate reused original ID: %+v", duplicate.Workspace)
	}
	if out, err := s.cli("workspace", "rename", duplicate.Workspace.WorkspaceID, "HQ-copy"); err != nil {
		t.Fatalf("rename duplicate: %s %v", out, err)
	}
	s.waitFor("duplicate and single HQ", 15*time.Second, func(text string) bool {
		return strings.Count(text, "🎏 oyagoi") == 1 && strings.Count(text, "HQ-original") == 1 && strings.Count(text, "HQ-copy") == 1
	})
	_, headingY := s.find("🎏 oyagoi")
	_, originalY := s.find("HQ-original")
	_, copyY := s.find("HQ-copy")
	if !(headingY < originalY && originalY < copyY) {
		t.Fatalf("unexpected HQ ordering: heading=%d original=%d copy=%d\n%s", headingY, originalY, copyY, s.text())
	}
}
