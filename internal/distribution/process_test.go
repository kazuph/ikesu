package distribution

import (
	"context"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"
)

// Reuse the fixture's ten-second readiness/socket deadline. Commands get two
// intervals, covering ensure's own fifteen-second run timeout.
const fixtureWait = 10 * time.Second

func fixtureEnvironment(home string, extra ...string) []string {
	values := map[string]string{"HOME": home, "PATH": os.Getenv("PATH"), "SHELL": "/bin/sh", "LANG": "C", "TMPDIR": home}
	for _, entry := range extra {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func fixtureCommand(t *testing.T, env []string, argv ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*fixtureWait)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.WaitDelay = fixtureWait
	return cmd
}

func fixtureNode(t *testing.T) string {
	t.Helper()
	// Resolve the selected runtime before changing HOME. A mise shim would
	// otherwise install Node again inside each isolated fixture directory.
	output, err := fixtureCommand(t, fixtureEnvironment(os.Getenv("HOME")),
		"node", "-p", "process.execPath").Output()
	if err != nil {
		t.Fatalf("resolve real Node runtime: %v: %s", err, output)
	}
	binary := strings.TrimSpace(string(output))
	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		t.Fatalf("invalid real Node runtime %q: %v", binary, err)
	}
	return binary
}
