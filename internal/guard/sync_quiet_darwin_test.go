//go:build darwin

package guard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQuietPeriodicSyncDoesNotRepeatIntegrityRead(t *testing.T) {
	root := t.TempDir()
	a := NewAssets(root, "p_1", "/usr/bin/true", Config{Targets: []string{"git"}})
	if _, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1"}, root); err != nil {
		t.Fatal(err)
	}
	defer a.Close()
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
	before := a.intactChecks
	for n := 0; n < 5; n++ {
		a.checkedAt = time.Now().Add(-guardAncestorCheckInterval)
		if err := a.Sync("codex"); err != nil {
			t.Fatal(err)
		}
		if time.Since(a.checkedAt) >= guardAncestorCheckInterval {
			t.Fatal("ancestor deadline was not refreshed")
		}
	}
	if got := a.intactChecks - before; got != 0 {
		t.Fatalf("five quiet intervals repeated %d full integrity reads; want 0", got)
	}
}

func BenchmarkQuietPeriodicSync(b *testing.B) {
	root := b.TempDir()
	a := NewAssets(root, "p_1", "/usr/bin/true", Config{Targets: []string{"git"}})
	if _, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1"}, root); err != nil {
		b.Fatal(err)
	}
	defer a.Close()
	if err := a.Sync("codex"); err != nil {
		b.Fatal(err)
	}
	if !a.ObserveChanges(func() {}) {
		b.Fatal("native watch unavailable")
	}
	for n := 0; n < 10 && a.checkedAssets == nil; n++ {
		a.checkedAt = time.Now().Add(-guardAncestorCheckInterval)
		if err := a.Sync("codex"); err != nil {
			b.Fatal(err)
		}
	}
	if a.checkedAssets == nil {
		b.Fatal("stable integrity snapshot was not established")
	}
	before := a.intactChecks
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		a.checkedAt = time.Now().Add(-guardAncestorCheckInterval)
		if err := a.Sync("codex"); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(a.intactChecks-before)/float64(b.N), "integrity_checks/op")
}

func TestPeriodicSyncWaitsForGenerationPublication(t *testing.T) {
	root := t.TempDir()
	a := NewAssets(root, "p_1", "/usr/bin/true", Config{Targets: []string{"git"}})
	if _, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1"}, root); err != nil {
		t.Fatal(err)
	}
	defer a.Close()
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
	shell, err := a.privateShell(false)
	if err != nil {
		t.Fatal(err)
	}
	defer shell.close()
	unlock, err := shell.lock(true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	a.checkedAt = time.Now().Add(-guardAncestorCheckInterval)
	done := make(chan error, 1)
	go func() { done <- a.Sync("codex") }()
	select {
	case err := <-done:
		t.Fatalf("sync returned during publication: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	unlock = nil
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sync did not resume after publication")
	}
}

func TestPeriodicSyncDetectsTamperingWithCachedIntegrity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   func(*Assets) string
		change func(string) error
	}{
		{"marker-written", func(a *Assets) string { return a.MarkerPath }, func(p string) error { return os.WriteFile(p, []byte("{}"), 0600) }},
		{"marker-deleted", func(a *Assets) string { return a.MarkerPath }, os.Remove},
		{"marker-replaced", func(a *Assets) string { return a.MarkerPath }, func(p string) error {
			if err := os.WriteFile(p+".replacement", []byte("{}"), 0600); err != nil {
				return err
			}
			return os.Rename(p+".replacement", p)
		}},
		{"loader-written", func(a *Assets) string { return filepath.Join(filepath.Dir(a.RuntimePath()), ".zshrc") }, func(p string) error { return os.WriteFile(p, []byte("corrupt"), 0600) }},
		{"payload-written", func(a *Assets) string {
			return filepath.Join(filepath.Dir(a.RuntimePath()), "current", ".zshrc.current")
		}, func(p string) error { return os.WriteFile(p, []byte("corrupt"), 0600) }},
		{"lock-permissions", func(a *Assets) string { return filepath.Join(filepath.Dir(a.RuntimePath()), "generation.lock") }, func(p string) error { return os.Chmod(p, 0666) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			a := NewAssets(root, "p_1", "/usr/bin/true", Config{Targets: []string{"git"}})
			if _, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1"}, root); err != nil {
				t.Fatal(err)
			}
			defer a.Close()
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
			before := a.intactChecks
			if err := tc.change(tc.path(a)); err != nil {
				t.Fatal(err)
			}
			a.checkedAt = time.Now().Add(-guardAncestorCheckInterval)
			if err := a.Sync("codex"); err != nil && a.issue == "" {
				t.Fatal(err)
			}
			if a.intactChecks == before && a.issue == "" {
				t.Fatal("tampering did not trigger integrity verification or an issue")
			}
			if a.issue == "" && !a.intact() {
				t.Fatal("tampered assets were neither repaired nor reported")
			}
		})
	}
}
