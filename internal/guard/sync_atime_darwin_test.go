//go:build darwin

package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func quietAssetsWithExecutable(t *testing.T) (*Assets, string) {
	t.Helper()
	root := t.TempDir()
	exe := filepath.Join(t.TempDir(), "ikesu")
	b, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, b, 0o700); err != nil {
		t.Fatal(err)
	}
	a := NewAssets(root, "p_1", exe, Config{Targets: []string{"git"}})
	if _, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1"}, root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if err := a.Sync("codex"); err != nil {
		t.Fatal(err)
	}
	if !a.ObserveChanges(func() {}) {
		t.Fatal("native watch unavailable")
	}
	for n := 0; n < 10 && a.checkedAssets == nil; n++ {
		a.checkedAt = time.Now().Add(-guardAncestorCheckInterval)
		if err := a.Sync("codex"); err != nil {
			t.Fatal(err)
		}
	}
	if a.checkedAssets == nil {
		t.Fatal("stable integrity snapshot was not established")
	}
	return a, exe
}

// Agents run the ikesu CLI constantly; each run only updates the executable's
// access time. That must not force the full integrity read on every pane.
func TestExecutableAccessTimeDoesNotForceIntegrityRead(t *testing.T) {
	a, exe := quietAssetsWithExecutable(t)
	before := a.intactChecks
	for n := 0; n < 5; n++ {
		// Running the file changes only its access time, as the CLI does.
		prev := accessTime(t, exe)
		time.Sleep(10 * time.Millisecond)
		if err := exec.Command(exe).Run(); err != nil {
			t.Fatal(err)
		}
		if accessTime(t, exe).Equal(prev) {
			t.Skip("this filesystem does not update the access time on exec")
		}
		a.checkedAt = time.Now().Add(-guardAncestorCheckInterval)
		if err := a.Sync("codex"); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.intactChecks - before; got != 0 {
		t.Fatalf("access-time-only changes caused %d full integrity reads; want 0", got)
	}
}

// A real change to the executable (modification time and status change)
// still sends the next periodic check to the full integrity read.
func TestExecutableModificationStillForcesIntegrityRead(t *testing.T) {
	a, exe := quietAssetsWithExecutable(t)
	before := a.intactChecks
	mt := time.Now().Add(time.Hour)
	if err := os.Chtimes(exe, mt, mt); err != nil {
		t.Fatal(err)
	}
	a.checkedAt = time.Now().Add(-guardAncestorCheckInterval)
	if err := a.Sync("codex"); err != nil {
		t.Fatal(err)
	}
	if got := a.intactChecks - before; got == 0 {
		t.Fatal("executable modification skipped the full integrity read")
	}
}

func accessTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	return time.Unix(st.Atimespec.Sec, st.Atimespec.Nsec)
}
