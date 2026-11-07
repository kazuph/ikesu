//go:build darwin

package guard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPeriodicSyncDetectsSymlinkTargetChanges(t *testing.T) {
	cases := []struct {
		name   string
		marker bool
		change func(string) error
	}{
		{"marker-write", true, func(p string) error { return os.WriteFile(p, []byte("{}"), 0600) }},
		{"marker-delete", true, os.Remove},
		{"marker-replace", true, func(p string) error {
			if err := os.WriteFile(p+".new", []byte("{}"), 0600); err != nil {
				return err
			}
			return os.Rename(p+".new", p)
		}},
		{"shim-delete", false, os.Remove},
		{"shim-remove-execute", false, func(p string) error { return os.Chmod(p, 0600) }},
	}
	for _, tc := range cases {
		for _, notify := range []bool{false, true} {
			name := tc.name + "/notification-absent"
			if notify {
				name = tc.name + "/notification-present"
			}
			t.Run(name, func(t *testing.T) {
				root, outside := t.TempDir(), t.TempDir()
				a := NewAssets(root, "p_1", "/usr/bin/true", Config{Targets: []string{"git"}})
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
					t.Fatal("native watch unavailable")
				}
				link := a.MarkerPath
				mode := os.FileMode(0600)
				if !tc.marker {
					link, mode = filepath.Join(a.Bin, "git"), 0700
				}
				data, err := os.ReadFile(link)
				if err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(outside, "target")
				if err := os.WriteFile(target, data, mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
				for n := 0; n < 10; n++ {
					a.checkedAt = time.Now().Add(-guardAncestorCheckInterval)
					if err := a.Sync("codex"); err != nil {
						t.Fatal(err)
					}
					if a.checkedAssets != nil {
						break
					}
				}
				if !a.intact() {
					t.Fatal("valid symlink was not accepted before mutation")
				}
				t.Logf("reusable snapshot before mutation: %t", a.checkedAssets != nil)
				a.refreshChanges()
				for {
					select {
					case <-changes:
					default:
						goto drained
					}
				}
			drained:
				before := a.watchGeneration.Load()
				if err := tc.change(target); err != nil {
					t.Fatal(err)
				}
				deadline := time.NewTimer(5 * time.Second)
				defer deadline.Stop()
				for a.watchGeneration.Load() == before {
					select {
					case <-changes:
					case <-deadline.C:
						t.Fatal("target change did not deliver native notification")
					}
				}
				if !notify {
					// Consume the real notification before the deadline; the next
					// periodic check must also be safe without another event.
					a.checkedAt = time.Now()
					if err := a.Sync("codex"); err != nil && a.issue == "" {
						t.Fatal(err)
					}
					for n := 0; n < 10 && a.checkedGeneration != a.watchGeneration.Load(); n++ {
						if err := a.Sync("codex"); err != nil && a.issue == "" {
							t.Fatal(err)
						}
					}
					if a.checkedGeneration != a.watchGeneration.Load() {
						t.Fatal("notification remained pending before periodic check")
					}
				} else if a.checkedGeneration == a.watchGeneration.Load() {
					t.Fatal("no notification was pending before periodic check")
				}
				a.checkedAt = time.Now().Add(-guardAncestorCheckInterval)
				if err := a.Sync("codex"); err != nil && a.issue == "" {
					t.Fatal(err)
				}
				if a.issue == "" && !a.intact() {
					t.Fatal("symlink target corruption was neither repaired nor reported")
				}
			})
		}
	}
}
