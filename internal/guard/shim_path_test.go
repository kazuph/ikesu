package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteRemovesEveryShimWithoutMarkerVariables(t *testing.T) {
	if os.Getenv("GTX_EXECUTE_TEST_CHILD") == "1" {
		if err := Execute(Input{Command: "sh", Args: []string{"-c", "printf '%s\\n' REAL_EXEC \"$PATH\"; command -v sh"}, Cwd: os.Getenv("GTX_EXECUTE_TEST_CWD"), Env: EnvMap(os.Environ())}); err != nil {
			t.Fatal(err)
		}
		return
	}
	root := t.TempDir()
	state := filepath.Join(root, "state")
	paths := []string{filepath.Join(state, "guard", "bin", "p1"), filepath.Join(state, "guard", "bin", "p2")}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "sh"), []byte("#!/bin/sh\nexit 93\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, cmd := range []string{"sh", filepath.Join(paths[0], "sh")} {
		env := []string{"PATH=" + strings.Join(paths, ":") + ":/usr/bin:/bin", "GTX_STATE_DIR=" + state}
		path, err := Resolve(cmd, env)
		if cmd != "sh" {
			if err == nil {
				t.Fatal("absolute shim accepted", path)
			}
			continue
		}
		if err != nil || strings.HasPrefix(path, state) {
			t.Fatal(path, err)
		}
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestExecuteRemovesEveryShimWithoutMarkerVariables$")
	// Explicit clean environment: the test cannot inherit the user's guard bins.
	cmd.Env = []string{"HOME=" + root, "PATH=" + strings.Join(paths, ":") + ":/usr/bin:/bin", "GTX_STATE_DIR=" + state, "GTX_EXECUTE_TEST_CHILD=1", "GTX_EXECUTE_TEST_CWD=" + root}
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.HasPrefix(string(output), "REAL_EXEC\n/usr/bin:/bin\n") {
		t.Fatal(string(output), err)
	}
	for _, path := range paths {
		if strings.Contains(string(output), path) {
			t.Fatal(string(output))
		}
	}
}
