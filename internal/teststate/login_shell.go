package teststate

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// GatedLoginShell runs a real shell whose login PATH query stays pending
// until the test releases it. Responsiveness is checked by event ordering,
// independently of how quickly the host schedules either process.
func GatedLoginShell(t *testing.T) (shell, started, release string) {
	t.Helper()
	dir := TempDir(t)
	shell, started, release = filepath.Join(dir, "shell"), filepath.Join(dir, "started"), filepath.Join(dir, "release")
	script := fmt.Sprintf("#!/bin/sh\nexport PATH=/usr/bin:/bin:$PATH\ncase \"$*\" in *-lic*) printf waiting > '%s'; while [ ! -f '%s' ]; do /bin/sleep 0.01; done;; esac\nexec /bin/sh \"$@\"\n", started, release)
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(release, []byte("release"), 0600) })
	return shell, started, release
}
