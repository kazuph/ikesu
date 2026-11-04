package teststate

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"regexp"
	"testing"
	"time"
)

// isolatedMarker names the one test an isolated child process runs.
const isolatedMarker = "GO_TEST_TESTSTATE_ISOLATED"

// RunIsolatedParallel runs a slow test in its own test process, in parallel
// with the other tests of the package. The child runs the same test body with
// the remaining package deadline and the same -test.parallel; a TestMain
// using Run gives it its own HOME and state directories. It returns true in
// the parent, which has already checked the child's result, and false in the
// child, which then runs the test body.
func RunIsolatedParallel(t *testing.T) bool {
	return runIsolatedParallelNamed(t, t.Name())
}

func runIsolatedParallelNamed(t *testing.T, name string) bool {
	t.Helper()
	if os.Getenv(isolatedMarker) == t.Name() {
		return false
	}
	t.Parallel()
	ctx := t.Context()
	args := []string{"-test.run=^" + regexp.QuoteMeta(name) + "$", "-test.v"}
	if parallel := flag.Lookup("test.parallel"); parallel != nil {
		args = append(args, "-test.parallel="+parallel.Value.String())
	}
	if deadline, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
		args = append(args, "-test.timeout="+time.Until(deadline).String())
	}
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Env = append(os.Environ(), isolatedMarker+"="+t.Name())
	cmd.WaitDelay = 5 * time.Second
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
