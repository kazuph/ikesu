package server

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"os/exec"
	"regexp"
	"testing"
)

// runIsolatedParallel gives slow real-server tests their own process-wide
// environment while retaining the original test and its real timeouts.
func runIsolatedParallel(t *testing.T) bool {
	t.Helper()
	const marker = "GO_TEST_SERVER_ISOLATED"
	if os.Getenv(marker) == t.Name() {
		return false
	}
	t.Parallel()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.v")
	cmd.Env = append(os.Environ(), marker+"="+t.Name())
	output, err := cmd.CombinedOutput()
	t.Logf("isolated test:\n%s", output)
	if err != nil {
		t.Fatalf("isolated test failed: %v", err)
	}
	return true
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
