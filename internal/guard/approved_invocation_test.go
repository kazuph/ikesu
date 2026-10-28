package guard

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestApprovedInvocationWritablePaths(t *testing.T) {
	i := Input{Command: "/bin/rm", Env: map[string]string{"PATH": "/usr/bin:/bin"}}
	for path := "/bin/rm"; ; path = filepath.Dir(path) {
		t.Logf("path=%s write-access=%v", path, unix.Access(path, unix.W_OK))
		if filepath.Dir(path) == path {
			break
		}
	}
	if os.Geteuid() != 0 && !immutableInvocation(i) {
		t.Fatal("system rm must preserve approved invocation")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "rm")
	if err := os.Symlink("/bin/rm", path); err != nil {
		t.Fatal(err)
	}
	i.Command = path
	if immutableInvocation(i) {
		t.Fatal("writable symlink container reused approval")
	}
}
