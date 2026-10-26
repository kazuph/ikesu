//go:build darwin

package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAssetChangesRepairAndRearm(t *testing.T) {
	root := t.TempDir()
	a := NewAssets(root, "p_1", "/usr/bin/true", Config{})
	if _, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1"}, root); err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Sync("codex"); err != nil {
		t.Fatal(err)
	}
	changes := make(chan struct{}, 1)
	if !a.ObserveChanges(func() {
		select {
		case changes <- struct{}{}:
		default:
		}
	}) {
		t.Fatal("native asset watch unavailable")
	}
	drain := func() {
		for {
			select {
			case <-changes:
			default:
				return
			}
		}
	}
	waitRepair := func(t *testing.T, check func() bool) {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-changes:
				if err := a.Sync("codex"); err != nil {
					t.Fatal(err)
				}
				if check() {
					return
				}
			case <-timer.C:
				t.Fatal("asset change did not trigger repair")
			}
		}
	}
	shim := filepath.Join(a.Bin, "git")
	exists := func(path string) bool { _, err := os.Stat(path); return err == nil }
	for _, change := range []struct {
		name   string
		mutate func() error
		intact func() bool
	}{
		{"marker-delete", func() error { return os.Remove(a.MarkerPath) }, func() bool { return exists(a.MarkerPath) }},
		{"shim-delete", func() error { return os.Remove(shim) }, func() bool { return exists(shim) }},
		{"marker-replace", func() error {
			path := a.MarkerPath + ".replacement"
			if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
				return err
			}
			return os.Rename(path, a.MarkerPath)
		}, func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.intact() }},
		{"bin-delete", func() error { return os.RemoveAll(a.Bin) }, func() bool { return exists(shim) }},
		{"guard-parent-delete", func() error { return os.RemoveAll(filepath.Join(a.Root, "guard")) }, func() bool { return exists(a.MarkerPath) && exists(shim) }},
		{"marker-parent-delete", func() error { return os.RemoveAll(filepath.Dir(a.MarkerPath)) }, func() bool { return exists(a.MarkerPath) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			drain()
			if err := change.mutate(); err != nil {
				t.Fatal(err)
			}
			waitRepair(t, change.intact)
		})
	}
	drain()
	if err := os.Chmod(shim, 0600); err != nil {
		t.Fatal(err)
	}
	waitRepair(t, func() bool {
		info, err := os.Stat(shim)
		return err == nil && info.Mode().Perm() == 0700
	})
	if out, err := exec.Command(shim, "--version").CombinedOutput(); err != nil {
		t.Fatalf("repaired shim cannot execute: %v %s", err, out)
	}
	if err := a.Sync(""); err != nil {
		t.Fatal(err)
	}
	if a.pathWatch != nil {
		t.Fatal("inactive guard retained filesystem watches")
	}
	if err := a.Sync("codex"); err != nil {
		t.Fatal(err)
	}
	drain()
	if err := os.Remove(a.MarkerPath); err != nil {
		t.Fatal(err)
	}
	waitRepair(t, func() bool { return exists(a.MarkerPath) })
	a.StopObserving()
	if a.pathWatch != nil {
		t.Fatal("stopped guard retained filesystem watches")
	}
}

func TestAssetChangesShareReplacedDirectories(t *testing.T) {
	root := t.TempDir()
	assets := []*Assets{NewAssets(root, "p_1", "/usr/bin/true", Config{}), NewAssets(root, "p_2", "/usr/bin/true", Config{})}
	changes := []chan struct{}{make(chan struct{}, 1), make(chan struct{}, 1)}
	for i, a := range assets {
		if _, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=" + a.Pane}, root); err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		if err := a.Sync("codex"); err != nil {
			t.Fatal(err)
		}
		notify := changes[i]
		if !a.ObserveChanges(func() {
			select {
			case notify <- struct{}{}:
			default:
			}
		}) {
			t.Fatal("native asset watch unavailable")
		}
	}
	for repeat := 0; repeat < 2; repeat++ {
		for _, ch := range changes {
			for len(ch) > 0 {
				<-ch
			}
		}
		if err := os.RemoveAll(filepath.Join(root, "guard", "agent-panes")); err != nil {
			t.Fatal(err)
		}
		for i, a := range assets {
			select {
			case <-changes[i]:
				if err := a.Sync("codex"); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(a.MarkerPath); err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("shared replacement lost a subscriber", i, repeat)
			}
		}
	}
}
