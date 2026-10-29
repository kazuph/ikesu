package guard

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// RuntimePath is outside PATH. Only the owning server publishes its native
// executable here; retaining its inode also preserves approval peer binding.
func (a *Assets) RuntimePath() string {
	return filepath.Join(a.Root, "guard", "shell", a.Pane, "ikesu")
}

func (a *Assets) publishRuntime() error {
	shell, err := a.privateShell(true)
	if err != nil {
		return err
	}
	defer shell.close()
	unlock, err := shell.lock(true)
	if err != nil {
		return err
	}
	defer unlock()
	_, err = a.ensureGeneration(shell, true)
	return err
}

// Never truncate an inode another shell is currently reading. Identical
// content keeps its inode and mtime, including repeated startup/CLI calls.
func writeGenerated(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm() == mode {
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
			return nil
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".startup-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// RefreshStartup is run by the binary reached through the fixed native entry.
// It does not republish that entry: callers cannot nominate an executable.
func RefreshStartup(i Input) error {
	_, active, err := ReadMarker(i)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	_, err = StartupGeneration(i)
	return err
}

func entryAssets(i Input) (*Assets, error) {
	marker := i.Env["IKESU_GUARD_MARKER"]
	if !filepath.IsAbs(marker) {
		return nil, fmt.Errorf("guard marker must be absolute")
	}
	number, err := strconv.Atoi(strings.TrimPrefix(i.Env["IKESU_PANE_ID"], "p"))
	if err != nil || number <= 0 || i.Env["IKESU_PANE_ID"] != "p"+strconv.Itoa(number) {
		return nil, fmt.Errorf("invalid guard pane")
	}
	pane := strings.TrimSuffix(filepath.Base(marker), ".json")
	if pane != "p_"+strconv.Itoa(number) && pane != i.Env["IKESU_PANE_ID"] {
		return nil, fmt.Errorf("invalid guard marker name")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(marker)))
	a := NewAssets(root, pane, "", Config{})
	if marker != a.MarkerPath {
		return nil, fmt.Errorf("guard marker path mismatch")
	}
	a.meta.Pane = i.Env["IKESU_PANE_ID"]
	a.Executable, err = os.Executable()
	return a, err
}

// The returned path names an immutable generation, not a mutable current link.
// The same inter-process lock covers identity validation, optional repair and
// publication. A previous binary can neither validate nor publish after a swap.
type generationLease struct {
	path   string
	shell  *privateDirectory
	unlock func()
}

func (l *generationLease) close() { l.unlock(); l.shell.close() }
func StartupGeneration(i Input) (string, error) {
	lease, err := openRuntimeGeneration(i)
	if err != nil {
		return "", err
	}
	defer lease.close()
	return lease.path, nil
}

func openRuntimeGeneration(i Input) (_ *generationLease, failure error) {
	a, err := entryAssets(i)
	if err != nil {
		return nil, err
	}
	if err := a.validateManaged(false); err != nil {
		return nil, err
	}
	shell, err := a.privateShell(false)
	if err != nil {
		return nil, err
	}

	unlock, err := shell.lock(true)
	if err != nil {
		shell.close()
		return nil, err
	}
	defer func() {
		if failure != nil {
			unlock()
			shell.close()
		}
	}()
	_, dir, g, err := loadGeneration(shell)
	if err != nil {
		return nil, err
	}
	defer dir.close()
	a.Config = g.Policy
	a.native = g.Native
	link, err := shell.readlink("ikesu")
	if err != nil || link != filepath.Join("current", "ikesu") {
		return nil, fmt.Errorf("guard runtime mismatch")
	}
	ready := a.generationMatches(dir, g, true)
	if !ready && !a.generationMatches(dir, g, false) {
		return nil, fmt.Errorf("guard runtime mismatch")
	}
	markers, err := a.markerDirectory()
	if err != nil {
		return nil, err
	}
	_, markerError := markers.read(filepath.Base(a.MarkerPath))
	markers.close()
	if markerError != nil && !os.IsNotExist(markerError) {
		return nil, markerError
	}
	if markerError == nil {
		m, _, err := ReadMarker(i)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(m.Policy, &a.Config) {
			return nil, fmt.Errorf("guard generation policy mismatch")
		}
	}
	a.Executable = g.Executable
	if ready {
		return &generationLease{dir.path, shell, unlock}, nil
	}
	path, err := a.ensureGeneration(shell, false)
	if err != nil {
		return nil, err
	}
	return &generationLease{path, shell, unlock}, nil
}

func markerAssets(i Input, m Marker) (*Assets, error) {
	a, err := entryAssets(i)
	if err != nil {
		return nil, err
	}
	if m.Policy == nil {
		return nil, fmt.Errorf("missing guard policy")
	}
	a.Config = *m.Policy
	a.meta = m
	shell, err := a.privateShell(false)
	if err != nil {
		return nil, err
	}
	defer shell.close()
	unlock, err := shell.lock(false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	_, dir, g, err := loadGeneration(shell)
	if err != nil {
		return nil, err
	}
	defer dir.close()
	a.native = g.Native
	if !a.generationMatches(dir, g, true) {
		return nil, fmt.Errorf("guard runtime mismatch")
	}
	a.Executable = g.Executable
	return a, nil
}

// Startup failures retain the existing fail-closed diagnostic and audit trail.
func RejectStartup(i Input, failure error, output io.Writer) {
	output = redactedWriter{output, i}
	marker, _, markerError := ReadMarker(i)
	rule := "guard-runtime"
	if markerError != nil {
		rule = "guard-marker"
	}
	fmt.Fprintln(output, "❌ 実行を停止しました:", failure)
	a, err := entryAssets(i)
	if err != nil {
		fmt.Fprintln(output, "guard log: untrusted storage:", err)
		return
	}
	if err = a.validateManaged(false); err != nil {
		fmt.Fprintln(output, "guard log: untrusted storage:", err)
		return
	}
	root := filepath.Dir(guardDirectory(i))
	if err := Log(root, i, marker, result("block", rule, failure.Error()), "startup"); err != nil {
		fmt.Fprintln(output, "guard log:", err)
	}
}
