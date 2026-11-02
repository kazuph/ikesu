package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// A pane-less `ikesu run` without --cwd runs in the directory it was invoked
// from (as herdr does), not in the server's directory; --cwd still wins.
func TestRunWithoutCwdUsesCallerDirectory(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "run-cwd", Agent: "codex", State: "idle"}, nil)

	caller, _ := filepath.EvalSymlinks(t.TempDir())
	explicit, _ := filepath.EvalSymlinks(t.TempDir())
	run := func(dir string, args ...string) api.RunStartedResult {
		t.Helper()
		cmd := exec.Command(binPath, append([]string{"--session", s.name, "run", "--caller", "p1"}, args...)...)
		cmd.Env = append(os.Environ(), "IKESU_STATE_DIR="+s.stateDir)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("ikesu run: %v\n%s", err, out)
		}
		var res api.RunStartedResult
		if err := json.Unmarshal(out, &res); err != nil {
			t.Fatalf("run output %q: %v", out, err)
		}
		return res
	}
	logOf := func(job, label string) string {
		path := filepath.Join(s.stateDir, "sessions", s.name, "job-logs", job+"-"+label+".log")
		var text string
		s.waitFor("job "+job+" finished", 10*time.Second, func(string) bool {
			b, _ := os.ReadFile(path)
			text = string(b)
			return strings.Contains(text, "exit_code:")
		})
		return text
	}

	implicit := run(caller, "--label", "implicit", "--", "pwd")
	if text := logOf(implicit.Job, "implicit"); !strings.Contains(text, "[stdout] "+caller+"\n") {
		t.Fatalf("implicit cwd: want %s\n%s", caller, text)
	}
	override := run(caller, "--label", "explicit", "--cwd", explicit, "--", "pwd")
	if text := logOf(override.Job, "explicit"); !strings.Contains(text, "[stdout] "+explicit+"\n") {
		t.Fatalf("explicit cwd: want %s\n%s", explicit, text)
	}
}
