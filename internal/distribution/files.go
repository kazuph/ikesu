package distribution

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const wrapperMarker = "#!/bin/sh\n# Managed by ikesu tb install\n"

func wrapperBytes(destination string) []byte {
	quoted := "'" + strings.ReplaceAll(filepath.Join(destination, "bin", "terminal-browser"), "'", "'\\''") + "'"
	return []byte(wrapperMarker + "exec " + quoted + " \"$@\"\n")
}

type fileSnapshot struct {
	exists bool
	data   []byte
	mode   os.FileMode
	link   string
}

func snapshotFile(file string) (fileSnapshot, error) {
	info, err := os.Lstat(file)
	if os.IsNotExist(err) {
		return fileSnapshot{}, nil
	}
	if err != nil {
		return fileSnapshot{}, err
	}
	s := fileSnapshot{exists: true, mode: info.Mode()}
	if info.Mode()&os.ModeSymlink != 0 {
		s.link, err = os.Readlink(file)
	} else if info.Mode().IsRegular() {
		s.data, err = os.ReadFile(file)
	} else {
		return s, fmt.Errorf("refusing non-file destination %s", file)
	}
	return s, err
}

func sameFile(a, b fileSnapshot) bool {
	return a.exists == b.exists && a.mode == b.mode && a.link == b.link && bytes.Equal(a.data, b.data)
}

func atomicFile(file string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(file), ".ikesu-install-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), file)
}

type fileChange struct {
	path     string
	previous fileSnapshot
	next     []byte
	mode     os.FileMode
}

func (c fileChange) apply() error {
	current, err := snapshotFile(c.path)
	if err != nil {
		return err
	}
	if !sameFile(current, c.previous) {
		return fmt.Errorf("installation conflict at %s", c.path)
	}
	return atomicFile(c.path, c.next, c.mode)
}

func (c fileChange) restore() error {
	current, err := snapshotFile(c.path)
	if err != nil {
		return err
	}
	if !sameFile(current, fileSnapshot{exists: true, data: c.next, mode: c.mode}) {
		return fmt.Errorf("rollback conflict: preserving changed file %s", c.path)
	}
	if !c.previous.exists {
		return os.Remove(c.path)
	}
	if c.previous.link != "" {
		f, err := os.CreateTemp(filepath.Dir(c.path), ".ikesu-link-*")
		if err != nil {
			return err
		}
		name := f.Name()
		f.Close()
		os.Remove(name)
		defer os.Remove(name)
		if err := os.Symlink(c.previous.link, name); err != nil {
			return err
		}
		return os.Rename(name, c.path)
	}
	return atomicFile(c.path, c.previous.data, c.previous.mode.Perm())
}
