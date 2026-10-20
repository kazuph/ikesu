package cli

import (
	"strings"
	"testing"
)

func TestServerReloadConfigHelpUsageAndNoServer(t *testing.T) {
	opts := Options{Getenv: testEnv(t.TempDir())}
	code, out, stderr := runCLI(t, opts, "server", "--help")
	if code != 0 || !strings.Contains(out+stderr, "ikesu server reload-config") {
		t.Fatal(code, out, stderr)
	}
	code, _, stderr = runCLI(t, opts, "server", "reload-config", "extra")
	if code != 2 || !strings.Contains(stderr, "usage: ikesu server reload-config") {
		t.Fatal(code, stderr)
	}
	code, out, stderr = runCLI(t, opts, "server", "reload-config")
	if code != 1 || out != "" || !strings.Contains(stderr, "server_not_running") || !strings.Contains(stderr, "cli:server:reload-config") {
		t.Fatal(code, out, stderr)
	}
}
