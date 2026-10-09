package plugin

import (
	"os/exec"
	"strings"
	"testing"
)

func TestBuildFailureKeepsSeparateBoundedStreamTails(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "printf 'DISCARDED_STDOUT'; head -c 800000 /dev/zero | tr '\\0' A; printf 'DISCARDED_STDERR' >&2; head -c 800000 /dev/zero | tr '\\0' B >&2; exit 3")
	cmd.Dir = t.TempDir()
	err := runBuild(cmd, "build.test", 0, 1)
	if err == nil {
		t.Fatal("build succeeded")
	}
	text := err.Error()
	stderr := strings.Index(text, "stderr: showing last 65536 bytes; earlier output omitted")
	stdout := strings.Index(text, "stdout: showing last 65536 bytes; earlier output omitted")
	if stderr < 0 || stdout < stderr || !strings.HasSuffix(text, "Plugin was not installed.") {
		t.Fatal("incorrect diagnostic order")
	}
	errTail := text[stderr+len("stderr: showing last 65536 bytes; earlier output omitted\n") : stdout]
	outTail := strings.TrimSuffix(text[stdout+len("stdout: showing last 65536 bytes; earlier output omitted\n"):], "\nPlugin was not installed.")
	if strings.TrimRight(errTail, "\n") != strings.Repeat("B", buildOutputLimit) || strings.TrimRight(outTail, "\n") != strings.Repeat("A", buildOutputLimit) {
		t.Fatal("did not preserve exactly the final 65536 bytes of each stream")
	}
	if !strings.Contains(text, "build: 1/1") || !strings.Contains(text, "status: exit status: 3") || !strings.Contains(text, "cwd: "+cmd.Dir) {
		t.Fatal("missing build details")
	}
}
