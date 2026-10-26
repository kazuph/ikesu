package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestLiveHandoffQuietMetadataKeepsMultipleExpiries(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(config, []byte("onboarding=false\n[ui.sidebar.agents]\nrows=[[\"$custom\"],[\"$keep\"],[\"state_text\"]]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := startEnv(t, map[string]string{"IKESU_CONFIG": config})
	s.waitFor("shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	first := "p1"
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right", Focus: false}, nil)
	s.rpc(api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: "p2", Text: "exec /bin/cat", Keys: []string{"enter"}}, nil)
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p2", Source: "expiry-test", Agent: "codex", State: "idle"}, nil)
	s.rpc("pane.report_metadata", map[string]any{"pane_id": "p2", "source": "persistent", "title": "KEEP", "tokens": map[string]string{"keep": "KEEP-TOKEN"}}, nil)
	s.rpc("pane.report_metadata", map[string]any{"pane_id": "p2", "source": "short", "title": "DROP", "state_labels": map[string]string{"idle": "SHORT-LABEL"}, "ttl_ms": (5 * time.Second).Milliseconds()}, nil)
	s.rpc("pane.report_metadata", map[string]any{"pane_id": "p2", "source": "long", "tokens": map[string]string{"custom": "LONG-TOKEN"}, "ttl_ms": (8 * time.Second).Milliseconds()}, nil)
	s.waitFor("initial metadata", 5*time.Second, func(text string) bool {
		return strings.Contains(text, "DROP") && strings.Contains(text, "LONG-TOKEN") && strings.Contains(text, "KEEP-TOKEN")
	})
	if out, err := s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
		t.Fatal(out, err)
	}
	// Reading decoded frames does not send input or ask the API to redraw.
	s.waitFor("short inherited metadata expires without input", 7*time.Second, func(text string) bool {
		return strings.Contains(text, "KEEP") && !strings.Contains(text, "DROP") && !strings.Contains(text, "SHORT-LABEL") && strings.Contains(text, "LONG-TOKEN")
	})
	s.waitFor("second inherited deadline expires without input", 5*time.Second, func(text string) bool {
		return !strings.Contains(text, "LONG-TOKEN") && strings.Contains(text, "KEEP-TOKEN")
	})
	if out, err := s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
		t.Fatal(out, err)
	}
	s.waitFor("persistent values survive reconnect", 5*time.Second, func(text string) bool {
		return strings.Contains(text, "KEEP") && strings.Contains(text, "KEEP-TOKEN") && !strings.Contains(text, "LONG-TOKEN")
	})
}
