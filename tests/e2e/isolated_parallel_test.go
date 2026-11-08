package e2e

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"testing"
	"time"
)

const (
	// isolatedMarker names the one test an isolated child process runs.
	isolatedMarker = "GO_TEST_E2E_ISOLATED"
	// isolatedBinary hands the parent's ikesu build to the child so the
	// child does not rebuild it.
	isolatedBinary = "GO_TEST_E2E_BIN"
)

// Evidence and debugging switches are read when the package loads, before
// teststate removes IKESU_* variables, so they are passed on explicitly.
var isolatedPassThrough = func() []string {
	var env []string
	for _, key := range []string{"IKESU_E2E_KEEP_FAILED", "IKESU_SIDEBAR_EVIDENCE", "IKESU_TITLE_EVIDENCE"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}()

// runIsolatedParallel runs a real-server test in its own test process, in
// parallel with the other tests. The child gets its own HOME and state
// directories from teststate, the same ikesu binary, the same test body and
// the remaining package deadline. It returns true in the parent, which has
// already checked the child's result; it returns false in the child, which
// then runs the test body.
func runIsolatedParallel(t *testing.T) bool {
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
	// Every session is named "e2e", and tests find their own server with
	// ps by "<binary> --session e2e server". A per-child hard link keeps that
	// command line unique to this test while running the same build. A clone
	// rather than a hard link gives it its own file as well as its own path.
	binary := filepath.Join(t.TempDir(), "ikesu")
	if err := cloneBinary(binPath, binary); err != nil {
		t.Fatalf("clone isolated ikesu binary: %v", err)
	}
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Env = append(os.Environ(), isolatedMarker+"="+t.Name(), isolatedBinary+"="+binary)
	cmd.Env = append(cmd.Env, isolatedPassThrough...)
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
