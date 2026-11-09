package teststate

import (
	"flag"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func runIsolatedProbe(t *testing.T, probe string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.v", "-test.parallel=3")
	cmd.Env = append(os.Environ(), probe+"=1")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestRunIsolatedParallelRejectsMissingTest(t *testing.T) {
	const probe = "GO_TEST_TESTSTATE_MISSING_PROBE"
	if os.Getenv(probe) == "1" {
		runIsolatedParallelNamed(t, t.Name()+"_missing")
		return
	}
	t.Parallel()
	output, err := runIsolatedProbe(t, probe)
	if err == nil || !strings.Contains(output, "isolated test did not report PASS") {
		t.Fatalf("missing test was not rejected: error=%v output=%s", err, output)
	}
}

func TestRunIsolatedParallelRejectsSkippedTest(t *testing.T) {
	const probe = "GO_TEST_TESTSTATE_SKIPPED_PROBE"
	if os.Getenv(probe) == "1" {
		if os.Getenv(isolatedMarker) == t.Name() {
			t.Logf("--- PASS: %s (0.00s)", t.Name())
			t.Skip("deliberate skip")
		}
		RunIsolatedParallel(t)
		return
	}
	t.Parallel()
	output, err := runIsolatedProbe(t, probe)
	if err == nil || !strings.Contains(output, "isolated test did not report PASS") {
		t.Fatalf("skipped test was not rejected: error=%v output=%s", err, output)
	}
}

func TestRunIsolatedParallelRejectsFailedTest(t *testing.T) {
	const probe = "GO_TEST_TESTSTATE_FAILED_PROBE"
	if os.Getenv(probe) == "1" {
		if os.Getenv(isolatedMarker) == t.Name() {
			t.Fatal("deliberate failure")
		}
		RunIsolatedParallel(t)
		return
	}
	t.Parallel()
	output, err := runIsolatedProbe(t, probe)
	if err == nil || !strings.Contains(output, "isolated test failed:") || !strings.Contains(output, "deliberate failure") {
		t.Fatalf("failed test was not rejected: error=%v output=%s", err, output)
	}
}

// The child keeps the parent's -test.parallel, so parallel subtests inside an
// isolated test keep the concurrency they had before.
func TestRunIsolatedParallelPassesParallelLimit(t *testing.T) {
	const probe = "GO_TEST_TESTSTATE_PARALLEL_PROBE"
	if os.Getenv(probe) == "1" {
		if os.Getenv(isolatedMarker) == t.Name() {
			if got := flagValue("test.parallel"); got != "3" {
				t.Fatalf("child -test.parallel = %q, want 3", got)
			}
			return
		}
		RunIsolatedParallel(t)
		return
	}
	t.Parallel()
	output, err := runIsolatedProbe(t, probe)
	if err != nil || !strings.Contains(output, "--- PASS: "+t.Name()+" ") {
		t.Fatalf("isolated child did not pass: error=%v output=%s", err, output)
	}
}

func TestRunIsolatedParallelSubtest(t *testing.T) {
	t.Run("child", func(t *testing.T) {
		if RunIsolatedParallel(t) {
			return
		}
		if os.Getenv(isolatedMarker) != t.Name() {
			t.Fatal("subtest did not run in its own process")
		}
	})
}

func flagValue(name string) string {
	if f := flag.Lookup(name); f != nil {
		return f.Value.String()
	}
	return ""
}
