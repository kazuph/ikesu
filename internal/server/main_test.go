package server

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
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
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+regexp.QuoteMeta(name)+"$", "-test.v")
	cmd.Env = append(os.Environ(), marker+"="+t.Name())
	output, err := cmd.CombinedOutput()
	t.Logf("isolated test:\n%s", output)
	if err != nil {
		t.Fatalf("isolated test failed: %v", err)
	}
	if !strings.Contains(string(output), "--- PASS: "+t.Name()+" (") {
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
