package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestRuntimeSnapshotRealDecisionDoesNotExposeValues(t *testing.T) {
	for _, action := range []string{"reject", "interrupt"} {
		t.Run(action, func(t *testing.T) {
			s := start(t)
			s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
			s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "runtime-snapshot-test", Agent: "claude", State: "idle"}, nil)
			root := s.stateDir
			script := filepath.Join(root, "runtime.sh")
			after := filepath.Join(root, "after")
			if err := os.WriteFile(script, []byte("T=qz\ncurl -X POST https://example.test --data \"$T\"\nprintf after > '"+after+"'\n"), 0600); err != nil {
				t.Fatal(err)
			}
			env := []string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_CONFIG=" + filepath.Join(root, "no-config"), "IKESU_STATE_DIR=" + root, "IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + filepath.Join(root, "guard", "agent-panes", "p_1.json"), "BASH_ENV=" + filepath.Join(root, "guard", "shell", "p_1", "bash-env"), "AI_GUARD_REQUEST_REASON=対象プロジェクト: ikesuの隔離試験。必要な理由: 非公開の値が表示されないことを確認。実施内容: 実行前に却下します。"}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/bin/bash", script)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
			cmd.WaitDelay = time.Second
			cmd.Env = env
			cmd.Dir = root
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			var pending api.DecisionListResult
			s.waitFor("runtime approval", 5*time.Second, func(string) bool {
				s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
				return len(pending.Decisions) == 1
			})
			d := pending.Decisions[0]
			processes, err := exec.Command("/bin/ps", "-axo", "pid=,ppid=,comm=").Output()
			if err != nil {
				t.Fatal(err)
			}
			checked := 0
			for _, row := range strings.Split(string(processes), "\n") {
				fields := strings.Fields(row)
				if len(fields) < 3 || fields[1] != strconv.Itoa(cmd.Process.Pid) || filepath.Base(fields[2]) != "ikesu" {
					continue
				}
				argv, err := exec.Command("/bin/ps", "-p", fields[0], "-o", "command=").Output()
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(argv), "qz") || !strings.Contains(string(argv), "$T") {
					t.Fatal("guard argv did not preserve the unexpanded command")
				}
				checked++
			}
			if checked != 1 {
				t.Fatalf("expected one real pending guard argv, got %d", checked)
			}
			encoded, _ := json.Marshal(d)
			if strings.Contains(string(encoded), "qz") || !strings.Contains(string(encoded), "$T") {
				t.Fatal("decision did not preserve original command", string(encoded))
			}
			if action == "reject" {
				if raw, err := s.cli("decision", "answer", d.DecisionID, "reject"); err != nil {
					t.Fatal(raw, err)
				}
			} else {
				if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			}
			if err := <-done; err == nil {
				t.Fatal("rejected command returned success")
			}
			if _, err := os.Stat(after); !os.IsNotExist(err) {
				t.Fatal("post-rejection line ran", err)
			}
			records, err := os.ReadFile(filepath.Join(root, "guard", "decisions.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(records), "qz") || strings.Contains(output.String(), "qz") {
				t.Fatal("snapshot leaked to log or stderr")
			}
			if !strings.Contains(string(records), "$T") {
				t.Fatal("original command missing in record")
			}
			t.Logf("real decision %s PASS decision=%s snapshot_value_exposures=0 after_exists=0", action, d.DecisionID)
		})
	}
}
