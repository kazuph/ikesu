//go:build darwin

package filewatch

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func atimeOf(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	return time.Unix(st.Atimespec.Sec, st.Atimespec.Nsec)
}

// Running a watched executable moves only its access time. Every guarded
// pane watches the ikesu runtime, and agents run the ikesu CLI constantly, so
// such an event must not wake the listeners; a permission change still does.
func TestExecutingWatchedFileDoesNotNotify(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "tool")
	b, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, b, 0o700); err != nil {
		t.Fatal(err)
	}
	w := New(nil)
	defer w.Close()
	if !w.Update([]string{exe}) {
		t.Fatal("watch unavailable")
	}
	// Let the registration settle before measuring.
	time.Sleep(100 * time.Millisecond)
	before := w.Revision()
	moved := false
	for n := 0; n < 5; n++ {
		prev := atimeOf(t, exe)
		time.Sleep(10 * time.Millisecond)
		if err := exec.Command(exe).Run(); err != nil {
			t.Fatal(err)
		}
		if !atimeOf(t, exe).Equal(prev) {
			moved = true
		}
	}
	if !moved {
		t.Skip("this filesystem does not update the access time on exec")
	}
	time.Sleep(200 * time.Millisecond)
	if got := w.Revision() - before; got != 0 {
		t.Fatalf("access-time-only changes notified %d times; want 0", got)
	}
	if err := os.Chmod(exe, 0o500); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for w.Revision() == before {
		if time.Now().After(deadline) {
			t.Fatal("permission change was not notified")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
