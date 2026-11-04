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

func TestGuardOutsidePaneDoesNotUseInheritedOrigin(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "outside-test", Agent: "claude", State: "idle"}, nil)
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	entry := filepath.Join(s.stateDir, "guard", "shell", "p_1", "ikesu")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, entry, "guard", "exec", "--", "dd", "if=/dev/null", "of=/dev/null")
	cmd.Env = []string{"HOME=" + s.stateDir, "PATH=/usr/bin:/bin", "IKESU_STATE_DIR=" + s.stateDir,
		"IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + marker,
		"IKESU_SOCKET_PATH=" + api.APISocketPath(filepath.Join(s.stateDir, s.name)), "IKESU_SOCKET_PATH_EXPLICIT=1",
		"AI_GUARD_REQUEST_REASON=隔離した試験で判断待ちの出所を確認します。"}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { cancel(); <-done }()
	var pending api.DecisionListResult
	s.waitFor("outside guard decision", 3*time.Second, func(string) bool {
		s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
		return len(pending.Decisions) == 1
	})
	d := pending.Decisions[0]
	if d.Origin == nil || d.Origin.PaneID != "" || d.Origin.PaneLabel != "" || d.Origin.WorkspaceLabel != "" || d.Origin.TabLabel != "" || d.Origin.Agent != "" {
		t.Fatalf("outside process inherited pane or agent origin: %+v", d.Origin)
	}
	if _, err := s.cli("decision", "answer", d.DecisionID, "reject"); err != nil {
		t.Fatal(err)
	}
}
