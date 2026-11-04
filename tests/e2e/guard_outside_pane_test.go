package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// A real isolated server and shell are needed to distinguish inherited pane
// identity from the kernel identity of an unrelated process.
func TestGuardOutsidePaneMissingMarkerSafeLine(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "outside-test", Agent: "claude", State: "idle"}, nil)
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	entry := filepath.Join(s.stateDir, "guard", "shell", "p_1", "ikesu")
	cmd := exec.CommandContext(ctx, entry, "guard", "line", "--", "true")
	cmd.Env = []string{"HOME=" + s.stateDir, "PATH=/usr/bin:/bin", "IKESU_STATE_DIR=" + s.stateDir,
		"IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + marker,
		"IKESU_SOCKET_PATH=" + api.APISocketPath(filepath.Join(s.stateDir, s.name)), "IKESU_SOCKET_PATH_EXPLICIT=1"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("safe line with stale marker must be evaluated as outside: %v: %s", err, out)
	}
}

func TestGuardOutsidePaneUnreachableServerStopsSafeCommand(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "outside-test", Agent: "claude", State: "idle"}, nil)
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	entry := filepath.Join(s.stateDir, "guard", "shell", "p_1", "ikesu")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, entry, "guard", "line", "--", "true")
	cmd.Env = []string{"HOME=" + s.stateDir, "PATH=/usr/bin:/bin", "IKESU_STATE_DIR=" + s.stateDir,
		"IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + marker,
		"IKESU_SOCKET_PATH=" + filepath.Join(s.stateDir, "missing.sock"), "IKESU_SOCKET_PATH_EXPLICIT=1"}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("unreachable identity server allowed a safe line: %s", out)
	}
}
