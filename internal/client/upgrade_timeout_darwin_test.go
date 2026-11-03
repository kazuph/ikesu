//go:build darwin

package client

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A bounded test context exercises the same timeout path without a ten-second wait.
func TestUpgradeVersionCheckStopsHungExecutable(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(home, ".ikesu-upgrade-timeout-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	data, err := os.ReadFile("/usr/bin/yes")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "yes")
	if err := os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("codesign", "--force", "--sign", "-", path).CombinedOutput(); err != nil {
		t.Fatalf("sign isolated executable copy: %v %s", err, out)
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_, err = checkUpgradeExecutable(ctx, path, "timeout-test")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hung --version did not time out: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 5*time.Second {
		t.Fatalf("version check test exceeded five seconds: %s", elapsed)
	}
}
