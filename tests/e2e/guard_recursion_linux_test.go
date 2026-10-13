//go:build linux

package e2e

import (
	"bytes"
	"context"
	"github.com/kazuph/goterrace/internal/guard"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The isolated process group is killed at this test-only safety ceiling.
// A normal guard→printf run needs one process and no recursive children.
const guardRecursionProcessLimit = 8

func TestGuardExecWithoutShimVariableRunsRealCommandOnce(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	config := filepath.Join(home, "config.toml")
	if err := os.WriteFile(config, []byte("[guard]\nmode='record'\ntargets=['printf']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var bins []string
	for _, pane := range []string{"p_1", "p_2"} {
		assets := guard.NewAssets(root, pane, binPath, guard.Config{Mode: "record", Targets: []string{"printf"}})
		if _, err := assets.Prepare([]string{"HOME=" + home, "GTX_PANE_ID=p1", "PATH=/usr/bin:/bin"}, home); err != nil {
			t.Fatal(err)
		}
		if err := assets.Sync("codex"); err != nil {
			t.Fatal(err)
		}
		defer assets.Close()
		bins = append(bins, assets.Bin)
	}
	env := []string{"HOME=" + home, "GTX_STATE_DIR=" + root, "GTX_CONFIG=" + config, "PATH=" + strings.Join(append(bins, "/usr/bin", "/bin"), ":")}
	// No GTX_GUARD_BIN / MARKER / other guard-state variable is present.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binPath, "guard", "exec", "--", "printf", "REAL_COMMAND_ONCE\\n")
	command.Env = env
	command.Dir = home
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	group := command.Process.Pid
	defer syscall.Kill(-group, syscall.SIGKILL)
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	maximum := 0
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if ctx.Err() != nil || err != nil {
				t.Fatal("bounded guard execution failed", ctx.Err(), err, output.String())
			}
			if output.String() != "REAL_COMMAND_ONCE\n" {
				t.Fatal("real command did not execute exactly once", output.String())
			}
			t.Logf("real_command_calls=1 max_process_group_members=%d limit=%d", maximum, guardRecursionProcessLimit)
			return
		case <-ticker.C:
			count := guardProcessGroupMembers(group)
			maximum = max(maximum, count)
			if count > guardRecursionProcessLimit {
				syscall.Kill(-group, syscall.SIGKILL)
				<-done
				t.Fatalf("recursive process limit exceeded: %d", count)
			}
		}
	}
}
func guardProcessGroupMembers(group int) int {
	entries, _ := os.ReadDir("/proc")
	count := 0
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) < 3 {
			continue
		}
		pgid, err := strconv.Atoi(fields[2])
		if err == nil && pgid == group {
			count++
		}
	}
	return count
}
