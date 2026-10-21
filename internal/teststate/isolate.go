package teststate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run isolates defaults before any tests or their child processes execute.
// Short roots also keep macOS Unix socket paths within the OS limit.
func Run(run func() int) (code int) {
	root, err := os.MkdirTemp("/tmp", "gtx-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ikesu test isolation:", err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			fmt.Fprintln(os.Stderr, "ikesu test cleanup:", err)
			code = 1
		}
	}()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GTX_") && !strings.HasPrefix(key, "GTX_PERF_") || strings.HasPrefix(key, "IKESU_") && !strings.HasPrefix(key, "IKESU_PERF_") || key == "IKESU" || key == "GOTERRACE" || key == "GOTERRACE_PANE_ID" {
			if err := os.Unsetenv(key); err != nil {
				fmt.Fprintln(os.Stderr, "ikesu test isolation:", err)
				return 1
			}
		}
	}
	paths := map[string]string{
		"HOME": "home", "ZDOTDIR": "home", "GTX_STATE_DIR": "state",
		"XDG_CONFIG_HOME": "config", "XDG_CONFIG_DIRS": "config",
		"XDG_DATA_HOME": "data", "XDG_DATA_DIRS": "data",
		"XDG_STATE_HOME": "xdg-state", "XDG_CACHE_HOME": "cache",
		"XDG_RUNTIME_DIR": "run",
	}
	for key, name := range paths {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(path, 0700); err != nil {
			fmt.Fprintln(os.Stderr, "ikesu test isolation:", err)
			return 1
		}
		if err := os.Setenv(key, path); err != nil {
			fmt.Fprintln(os.Stderr, "ikesu test isolation:", err)
			return 1
		}
	}
	return run()
}

func TempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "gtx-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}
