package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeRejectsUntrustedExistingDirectories(t *testing.T) {
	for _, kind := range []string{"state-writable", "guard-writable", "pane-symlink", "state-symlink"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "state")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			shared := filepath.Join(base, "shared")
			if err := os.Mkdir(shared, 0777); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(shared, 0777); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "state-writable":
				if err := os.Chmod(root, 0777); err != nil {
					t.Fatal(err)
				}
			case "guard-writable":
				if err := os.Mkdir(filepath.Join(root, "guard"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(root, "guard"), 0777); err != nil {
					t.Fatal(err)
				}
			case "pane-symlink":
				if err := os.MkdirAll(filepath.Join(root, "guard", "shell"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(shared, filepath.Join(root, "guard", "shell", "p_1")); err != nil {
					t.Fatal(err)
				}
			case "state-symlink":
				if err := os.Remove(root); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(shared, root); err != nil {
					t.Fatal(err)
				}
			}
			a := NewAssets(root, "p_1", "/usr/bin/true", Config{})
			if _, err := a.Prepare([]string{"IKESU_PANE_ID=p1", "PATH=/usr/bin:/bin"}, base); err == nil {
				t.Fatal("untrusted entry published")
			}
			entries, err := os.ReadDir(shared)
			if err != nil || len(entries) != 0 {
				t.Fatalf("untrusted target was changed: %v %v", entries, err)
			}
		})
	}
	t.Run("foreign-owner", func(t *testing.T) {
		root := "/usr"
		if os.Geteuid() == 0 {
			root = filepath.Join(t.TempDir(), "foreign")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(root, 1, 1); err != nil {
				t.Fatal(err)
			}
		}
		if d, err := openPrivateState(root, false); err == nil {
			d.close()
			t.Fatal("foreign owner trusted")
		}
	})
}

func TestRuntimePublicationUsesCheckedDirectoryHandle(t *testing.T) {
	root := t.TempDir()
	a := NewAssets(root, "p_1", "/usr/bin/true", Config{})
	if _, err := a.Prepare([]string{"IKESU_PANE_ID=p1", "PATH=/usr/bin:/bin"}, root); err != nil {
		t.Fatal(err)
	}
	dir, err := a.privateShell(false)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.close()
	held := filepath.Dir(a.RuntimePath()) + "-held"
	if err := os.Rename(filepath.Dir(a.RuntimePath()), held); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if err := os.Symlink(other, filepath.Dir(a.RuntimePath())); err != nil {
		t.Fatal(err)
	}
	if err := dir.write("checked-publication", []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(held, "checked-publication")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, "checked-publication")); !os.IsNotExist(err) {
		t.Fatalf("followed replacement link: %v", err)
	}
	if err := a.publishRuntime(); err == nil {
		t.Fatal("accepted switched directory symlink")
	}
}

func TestRuntimeSameAgentRepairsOnlyTrustedNative(t *testing.T) {
	_, a := scriptPane(t)
	for _, kind := range []string{"missing", "corrupt", "other-executable"} {
		t.Run(kind, func(t *testing.T) {
			if err := os.Remove(a.RuntimePath()); err != nil {
				t.Fatal(err)
			}
			if kind == "corrupt" {
				if err := os.WriteFile(a.RuntimePath(), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "other-executable" {
				if err := os.Symlink("/usr/bin/true", a.RuntimePath()); err != nil {
					t.Fatal(err)
				}
			}
			if a.intact() {
				t.Fatal("broken native entry considered intact")
			}
			if err := a.Sync("codex"); err != nil {
				t.Fatal(err)
			}
			actual, err := os.Stat(a.RuntimePath())
			expected, _ := os.Stat(a.Executable)
			if err != nil || !os.SameFile(actual, expected) || a.Issue() != "" {
				t.Fatalf("trusted native not repaired: %v %s", err, a.Issue())
			}
		})
	}
}

func TestRuntimeRebindFailureRestoresPreviousGeneration(t *testing.T) {
	root := t.TempDir()
	a := NewAssets(root, "p_1", "/usr/bin/true", Config{})
	if _, err := a.Prepare([]string{"IKESU_PANE_ID=p1", "PATH=/usr/bin:/bin", "IKESU_SOCKET_PATH=" + root + "/old.sock"}, root); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync("codex"); err != nil {
		t.Fatal(err)
	}
	old, err := os.Readlink(filepath.Join(filepath.Dir(a.RuntimePath()), "current"))
	if err != nil {
		t.Fatal(err)
	}
	// A real rename failure after the new native generation has been published:
	// the destination marker is now a directory, so Rename cannot replace it.
	if err := os.Remove(a.MarkerPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(a.MarkerPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.RebindSocket(filepath.Join(root, "next.sock")); err == nil {
		t.Fatal("marker publication unexpectedly succeeded")
	}
	current, err := os.Readlink(filepath.Join(filepath.Dir(a.RuntimePath()), "current"))
	actual, _ := os.Stat(a.RuntimePath())
	expected, _ := os.Stat("/usr/bin/true")
	if err != nil || current != old || actual == nil || !os.SameFile(actual, expected) || a.Executable != "/usr/bin/true" {
		t.Fatalf("failed rebind retained new generation: %s %s %v", old, current, err)
	}
	if _, err := os.Stat(a.issuePath()); !os.IsNotExist(err) {
		t.Fatalf("rollback retained new coverage issue: %v", err)
	}
}
