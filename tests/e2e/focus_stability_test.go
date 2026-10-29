package e2e

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real terminal clients must send input to the same pane after reconnecting
// to a live-handoff server, despite having independent workspace/tab choices.
func TestFocusStabilityTwoTerminalsAcrossLiveHandoff(t *testing.T) {
	t.Parallel()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n[keys]\nswitch_workspace='cmd+1..9'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first := startEnv(t, map[string]string{"IKESU_CONFIG": cfg})
	first.waitFor("first shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	second := startEnv(t, map[string]string{"IKESU_STATE_DIR": first.stateDir, "IKESU_CONFIG": cfg})
	second.waitFor("second shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	// Use the normal CLI with a harmless real program. No child AI is launched.
	out, err := first.cli("agent", "start", "focus-stable-background", "--", "/bin/cat")
	if err != nil {
		t.Fatalf("default agent start: %s %v", out, err)
	}
	proof := func(s *session, pane, label string) {
		t.Helper()
		marker := "FOCUS_PROOF_" + label
		s.send(fmt.Sprintf("printf 'FOCUS_PROOF_%%s\\n' '%s'\r", label))
		s.waitFor(label, 5*time.Second, func(text string) bool { return strings.Contains(text, marker) })
		var read api.PaneReadResultEnvelope
		first.rpc(api.MethodPaneRead, api.PaneReadParams{PaneID: pane, StripANSI: true, Source: "recent", Format: "text"}, &read)
		if !strings.Contains(read.Read.Text, marker) {
			t.Fatalf("%s input went to another pane (expected %s): %s", label, pane, read.Read.Text)
		}
		t.Logf("%s selected pane=%s", label, pane)
	}
	proof(first, "p1", "DEFAULT_AGENT_KEEPS_FIRST")
	proof(second, "p1", "DEFAULT_AGENT_KEEPS_SECOND")
	// Build distinct workspace/tab/pane selections through the ordinary UI.
	second.send("\x02c\r")
	second.waitFor("second terminal new tab", 5*time.Second, func(text string) bool { return strings.Contains(text, "%3 terminal") && strings.Contains(text, "❯") })
	var space api.WorkspaceCreatedResult
	first.rpc(api.MethodWorkspaceCreate, api.WorkspaceCreateParams{}, &space)
	// Select this workspace only on the first terminal using its configured key binding.
	first.send("\x1b[50;9u")
	first.waitFor("first other workspace", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") && !strings.Contains(text, "%3 terminal") })
	proof(first, space.RootPane.PaneID, "FIRST_BEFORE_HANDOFF")
	proof(second, "p3", "SECOND_BEFORE_HANDOFF")
	if out, err := first.cli("server", "live-handoff"); err != nil {
		t.Fatalf("handoff: %s %v", out, err)
	}
	proof(first, space.RootPane.PaneID, "FIRST_AFTER_HANDOFF")
	proof(second, "p3", "SECOND_AFTER_HANDOFF")
	t.Logf("first retained %s/%s/%s; second retained s1/s1:t3/p3", space.Workspace.WorkspaceID, space.Tab.TabID, space.RootPane.PaneID)
	// --focus and existing --no-focus remain normal CLI operations.
	if out, err := first.cli("agent", "start", "focus-stable-explicit", "--focus", "--", "/bin/sh"); err != nil {
		t.Fatalf("focus: %s %v", out, err)
	}
	proof(first, "p5", "EXPLICIT_FOCUS_FIRST")
	proof(second, "p5", "EXPLICIT_FOCUS_SECOND")
	if out, err := first.cli("agent", "start", "focus-stable-old-flag", "--no-focus", "--", "/bin/cat"); err != nil {
		t.Fatalf("no-focus: %s %v", out, err)
	}
	proof(first, "p5", "NO_FOCUS_FIRST")
	proof(second, "p5", "NO_FOCUS_SECOND")
}
