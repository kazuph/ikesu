package guard

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RuntimePath is outside PATH. Only the owning server publishes its native
// executable here; retaining its inode also preserves approval peer binding.
func (a *Assets) RuntimePath() string {
	return filepath.Join(a.Root, "guard", "shell", a.Pane, "ikesu")
}

func (a *Assets) publishRuntime() error {
	if !filepath.IsAbs(a.Executable) {
		return fmt.Errorf("guard executable must be absolute")
	}
	info, err := os.Stat(a.Executable)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return fmt.Errorf("guard executable is not executable")
	}
	target, err := filepath.EvalSymlinks(a.Executable)
	if err != nil {
		return err
	}
	path := a.RuntimePath()
	if old, err := os.Readlink(path); err == nil && old == target {
		return nil
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".runtime-")
	if err != nil {
		return err
	}
	name := f.Name()
	if err = f.Close(); err != nil {
		os.Remove(name)
		return err
	}
	defer os.Remove(name)
	if err = os.Remove(name); err != nil {
		return err
	}
	if err = os.Symlink(target, name); err != nil {
		return err
	}
	return os.Rename(name, path)
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
	m, active, err := ReadMarker(i)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	a, err := markerAssets(i, m)
	if err != nil {
		return err
	}
	return a.prepareStartup(map[string]string{}, filepath.Dir(a.RuntimePath()))
}

func markerAssets(i Input, m Marker) (*Assets, error) {
	root := filepath.Dir(filepath.Dir(filepath.Dir(i.Env["IKESU_GUARD_MARKER"])))
	number, err := strconv.Atoi(strings.TrimPrefix(m.Pane, "p"))
	if err != nil || number <= 0 || m.Pane != "p"+strconv.Itoa(number) || m.Policy == nil {
		return nil, fmt.Errorf("invalid guard pane")
	}
	pane := "p_" + strconv.Itoa(number)
	a := NewAssets(root, pane, "", *m.Policy)
	a.meta = m
	if filepath.Clean(i.Env["IKESU_GUARD_MARKER"]) != a.MarkerPath {
		return nil, fmt.Errorf("guard marker path mismatch")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	current, err := os.Stat(a.RuntimePath())
	if err != nil {
		return nil, err
	}
	own, err := os.Stat(exe)
	if err != nil || !os.SameFile(current, own) {
		return nil, fmt.Errorf("guard runtime mismatch")
	}
	a.Executable = exe
	return a, nil
}
