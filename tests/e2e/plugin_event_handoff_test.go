package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// This test starts real servers, queues hooks, and exercises a successful and a failed handoff.
func TestPluginEventsAcrossLiveHandoff(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := startEnv(t, map[string]string{"IKESU_CONFIG": cfg})
	s.waitFor("shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.rpc(api.MethodWorkspaceCreate, api.WorkspaceCreateParams{}, nil)
	s.rpc(api.MethodWorkspaceFocus, api.WorkspaceTarget{WorkspaceID: "s2"}, nil)
	root := t.TempDir()
	output := filepath.Join(root, "events")
	detail := filepath.Join(root, "event-detail")
	manifest := "id=\"handoff.events\"\nname=\"Handoff Events\"\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\n"
	const hooks = 12
	for n := 0; n < hooks; n++ {
		command := fmt.Sprintf("printf x >> %s; printf '%%s\\n' \"$IKESU_PLUGIN_EVENT_JSON\" >> %s", output, detail)
		manifest += fmt.Sprintf("[[events]]\non=\"workspace.focused\"\ncommand=[\"/bin/sh\",\"-c\",%q]\n", command)
	}
	if err := os.WriteFile(filepath.Join(root, "ikesu-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	s.rpc("plugin.link", map[string]any{"path": root}, nil)
	accepted := 0
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		workspace := "s1"
		if accepted%2 == 1 {
			workspace = "s2"
		}
		s.rpc(api.MethodWorkspaceFocus, api.WorkspaceTarget{WorkspaceID: workspace}, nil)
		accepted++
		logs, err := filepath.Glob(filepath.Join(s.stateDir, "plugins", "handoff.events", "command-*.json"))
		if err != nil {
			t.Fatal(err)
		}
		if accepted*hooks-len(logs) > 2*hooks {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("could not observe queued events")
		default:
		}
	}
	t.Logf("accepted %d events with hooks still queued", accepted)
	if out, err := s.cli("server", "live-handoff"); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	workspace := "s1"
	if accepted%2 == 1 {
		workspace = "s2"
	}
	s.rpc(api.MethodWorkspaceFocus, api.WorkspaceTarget{WorkspaceID: workspace}, nil)
	accepted++
	if out, err := s.cli("server", "live-handoff", "--import-exe", "/usr/bin/false"); err == nil {
		t.Fatalf("expected rejected handoff: %s", out)
	}
	workspace = "s1"
	if accepted%2 == 1 {
		workspace = "s2"
	}
	s.rpc(api.MethodWorkspaceFocus, api.WorkspaceTarget{WorkspaceID: workspace}, nil)
	accepted++
	wait := time.NewTimer(10 * time.Second)
	defer wait.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	want := accepted * hooks
	for {
		data, _ := os.ReadFile(output)
		if len(data) == want {
			return
		}
		if len(data) > want {
			t.Fatalf("duplicated hooks: got %d want %d", len(data), want)
		}
		select {
		case <-tick.C:
		case <-wait.C:
			var records struct {
				Logs []struct {
					Status string  `json:"status"`
					Error  *string `json:"error"`
				} `json:"logs"`
			}
			s.rpc("plugin.log.list", map[string]any{"plugin_id": "handoff.events"}, &records)
			counts := map[string]int{}
			for _, record := range records.Logs {
				counts[record.Status]++
			}
			details, _ := os.ReadFile(detail)
			t.Fatalf("completed %d hooks, want %d; logs=%v details=%s", len(data), want, counts, details)
		}
	}
}
