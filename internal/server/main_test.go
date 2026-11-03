package server

import (
	"context"
	"fmt"
	"github.com/kazuph/ikesu/internal/teststate"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// runIsolatedParallel gives slow real-server tests their own process-wide
// environment while retaining the original test and its real timeouts.
func runIsolatedParallel(t *testing.T) bool {
	return runIsolatedParallelNamed(t, t.Name())
}

func runIsolatedParallelNamed(t *testing.T, name string) bool {
	t.Helper()
	const marker = "GO_TEST_SERVER_ISOLATED"
	if os.Getenv(marker) == t.Name() {
		return false
	}
	t.Parallel()
	ctx := t.Context()
	args := []string{"-test.run=^" + regexp.QuoteMeta(name) + "$", "-test.v"}
	if deadline, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
		args = append(args, "-test.timeout="+time.Until(deadline).String())
	}
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Env = append(os.Environ(), marker+"="+t.Name())
	output, err := cmd.CombinedOutput()
	t.Logf("isolated test:\n%s", output)
	if err != nil {
		t.Fatalf("isolated test failed: %v", err)
	}
	quoted := regexp.QuoteMeta(t.Name())
	if !regexp.MustCompile(`(?m)^=== RUN   `+quoted+`\r?$`).Match(output) ||
		!regexp.MustCompile(`(?m)^--- PASS: `+quoted+` \([^\r\n]*\)\r?$`).Match(output) {
		t.Fatalf("isolated test did not report PASS: %s", t.Name())
	}
	return true
}

func TestIsolatedParallelRejectsMissingTest(t *testing.T) {
	const probe = "GO_TEST_MISSING_ISOLATED_PROBE"
	if os.Getenv(probe) == "1" {
		runIsolatedParallelNamed(t, t.Name()+"_missing")
		return
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.v")
	cmd.Env = append(os.Environ(), probe+"=1")
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "isolated test did not report PASS") {
		t.Fatalf("missing test was not rejected for missing PASS: error=%v output=%s", err, output)
	}
}

func TestIsolatedParallelRejectsSkippedTest(t *testing.T) {
	const probe = "GO_TEST_SKIPPED_ISOLATED_PROBE"
	if os.Getenv(probe) == "1" {
		if os.Getenv("GO_TEST_SERVER_ISOLATED") == t.Name() {
			t.Logf("--- PASS: %s (0.00s)", t.Name())
			t.Skip("deliberate skip")
		}
		runIsolatedParallel(t)
		return
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.v")
	cmd.Env = append(os.Environ(), probe+"=1")
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "isolated test did not report PASS") {
		t.Fatalf("skipped test was not rejected: error=%v output=%s", err, output)
	}
}

func TestIsolatedParallelRejectsFailedTest(t *testing.T) {
	const probe = "GO_TEST_FAILED_ISOLATED_PROBE"
	if os.Getenv(probe) == "1" {
		if os.Getenv("GO_TEST_SERVER_ISOLATED") == t.Name() {
			t.Fatal("deliberate failure")
		}
		runIsolatedParallel(t)
		return
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.v")
	cmd.Env = append(os.Environ(), probe+"=1")
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "isolated test failed:") || !strings.Contains(string(output), "deliberate failure") {
		t.Fatalf("failed test was not rejected: error=%v output=%s", err, output)
	}
}

func TestIsolatedParallelDeadlineReapsChild(t *testing.T) {
	const probe = "GO_TEST_ISOLATED_DEADLINE_PROBE"
	pidFile := os.Getenv(probe)
	if pidFile != "" {
		if os.Getenv("GO_TEST_SERVER_ISOLATED") == t.Name() {
			listener, err := net.Listen("unix", pidFile+".sock")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
				t.Fatal(err)
			}
			_, _ = listener.Accept()
			return
		}
		runIsolatedParallel(t)
		return
	}
	dir, err := os.MkdirTemp("/tmp", "isod-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	pidFile = filepath.Join(dir, "child.pid")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.v", "-test.timeout=3s")
	cmd.Env = append(os.Environ(), probe+"="+pidFile)
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("deadline probe unexpectedly passed: %s", output)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("deadline child did not start: %v, output=%s", err, output)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		state, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=", "-o", "command=").Output()
		fields := strings.Fields(string(state))
		if len(fields) == 0 || strings.HasPrefix(fields[0], "Z") {
			return
		}
		if time.Now().After(deadline) {
			pids, _ := exec.Command("pgrep", "-u", strconv.Itoa(os.Getuid()), "-f", regexp.QuoteMeta(os.Args[0])).Output()
			if len(fields) > 1 && fields[1] == os.Args[0] && strings.Contains("\n"+string(pids), "\n"+strconv.Itoa(pid)+"\n") {
				if process, err := os.FindProcess(pid); err == nil {
					_ = process.Kill()
				}
			}
			t.Fatalf("deadline left child %d running: %s", pid, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == jobRunnerArg {
		code, err := RunJobRunner()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ikesu job runner:", err)
		}
		os.Exit(code)
	}
	if len(os.Args) == 2 && os.Args[1] == jobSupervisorArg {
		code, _ := RunJobSupervisor()
		os.Exit(code)
	}
	os.Exit(teststate.Run(m.Run))
}
