//go:build darwin

package client

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The real command never exits for --version; this checks the full attachTimeout.
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
	_, err = checkUpgradeExecutable(context.Background(), path, "timeout-test")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hung --version did not time out: %v", err)
	}
}
