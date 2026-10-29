package guard

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Every managed component is opened relative to its already-checked parent.
// A symlink or a different owner's writable directory is never a repair target.
type privateDirectory struct {
	file *os.File
	path string
}

func (d *privateDirectory) close()  { d.file.Close() }
func (d *privateDirectory) fd() int { return int(d.file.Fd()) }
func privateStat(fd int, mask uint32) error {
	var s unix.Stat_t
	if err := unix.Fstat(fd, &s); err != nil {
		return err
	}
	if s.Mode&unix.S_IFMT != unix.S_IFDIR || int(s.Uid) != os.Geteuid() || uint32(s.Mode)&mask != 0 {
		return fmt.Errorf("guard directory is not private and owned by this user")
	}
	return nil
}

func isolatesDescendants(path string, s *unix.Stat_t) bool {
	// ACLs can grant traversal beyond the POSIX mode. Failure to read ACL
	// metadata must never establish that another user cannot reach descendants.
	const otherWriteOrSearch = unix.S_IWGRP | unix.S_IXGRP | unix.S_IWOTH | unix.S_IXOTH
	return s.Mode&unix.S_IFMT == unix.S_IFDIR && int(s.Uid) == os.Geteuid() &&
		s.Mode&otherWriteOrSearch == 0 && ancestorHasNoACL(path)
}

func trustedAncestors(path string) error {
	// Check lexical links as well as their resolved targets. Otherwise a foreign
	// owner could retarget an ancestor after its target had passed validation.
	var writable error
	for current := path; ; current = filepath.Dir(current) {
		var st unix.Stat_t
		if err := unix.Lstat(current, &st); err != nil {
			return err
		}
		if st.Uid != 0 && int(st.Uid) != os.Geteuid() {
			return fmt.Errorf("foreign guard ancestor: %s", current)
		}
		// Only an ACL-free directory that excludes other users protects descendants.
		// Still inspect its parents: an exposed writable parent could replace it.
		if isolatesDescendants(current, &st) {
			writable = nil
		}
		if st.Mode&unix.S_IFMT != unix.S_IFLNK && st.Mode&0022 != 0 && !(st.Uid == 0 && st.Mode&unix.S_ISVTX != 0) {
			writable = fmt.Errorf("writable guard ancestor: %s", current)
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	if writable != nil {
		return writable
	}
	for {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		var s unix.Stat_t
		if err = unix.Stat(path, &s); err != nil {
			return err
		}
		if !info.IsDir() || (int(s.Uid) != os.Geteuid() && s.Uid != 0) {
			return fmt.Errorf("untrusted guard ancestor: %s", path)
		}
		if isolatesDescendants(path, &s) {
			writable = nil
		}
		if s.Mode&0022 != 0 && !(s.Uid == 0 && s.Mode&unix.S_ISVTX != 0) {
			writable = fmt.Errorf("untrusted guard ancestor: %s", path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return writable
		}
		path = parent
	}
}
func openPrivateState(root string, create bool) (*privateDirectory, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := trustedAncestors(filepath.Dir(root)); err != nil {
		return nil, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(root))
	if err != nil {
		return nil, err
	}
	if err = trustedAncestors(parent); err != nil {
		return nil, err
	}
	fd, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	return privateChild(fd, parent, filepath.Base(root), create, false)
}
func privateChild(parent int, path, name string, create bool, private bool) (*privateDirectory, error) {
	if filepath.Base(name) != name || name == "." || name == ".." {
		return nil, fmt.Errorf("invalid guard directory component")
	}
	if create {
		if err := unix.Mkdirat(parent, name, 0700); err != nil && err != unix.EEXIST {
			return nil, err
		}
	}
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	mask := uint32(0022)
	if private {
		mask = 0077
	}
	if err = privateStat(fd, mask); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("%s: %w", filepath.Join(path, name), err)
	}
	return &privateDirectory{os.NewFile(uintptr(fd), name), filepath.Join(path, name)}, nil
}
func (d *privateDirectory) child(name string, create bool) (*privateDirectory, error) {
	return privateChild(d.fd(), d.path, name, create, true)
}
func (a *Assets) privateShell(create bool) (*privateDirectory, error) {
	d, err := openPrivateState(a.Root, create)
	if err != nil {
		return nil, err
	}
	for _, part := range []string{"guard", "shell", a.Pane} {
		next, e := d.child(part, create)
		d.close()
		if e != nil {
			return nil, e
		}
		d = next
	}
	return d, nil
}
func (d *privateDirectory) read(name string) ([]byte, error) {
	fd, err := unix.Openat(d.fd(), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	var s unix.Stat_t
	if err = unix.Fstat(fd, &s); err != nil {
		return nil, err
	}
	if s.Mode&unix.S_IFMT != unix.S_IFREG || int(s.Uid) != os.Geteuid() || s.Mode&0077 != 0 || s.Size > scriptFileLimit {
		return nil, fmt.Errorf("untrusted guard file: %s", name)
	}
	return io.ReadAll(f)
}
func (d *privateDirectory) write(name string, data []byte, mode os.FileMode) error {
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid guard file name")
	}
	if old, err := d.read(name); err == nil && string(old) == string(data) {
		var st unix.Stat_t
		if unix.Fstatat(d.fd(), name, &st, unix.AT_SYMLINK_NOFOLLOW) == nil && os.FileMode(st.Mode&0777) == mode {
			return nil
		}
	}
	tmp := ".write-" + rand.Text()
	fd, err := unix.Openat(d.fd(), tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode))
	if err != nil {
		return err
	}
	defer unix.Unlinkat(d.fd(), tmp, 0)
	f := os.NewFile(uintptr(fd), tmp)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if ce := f.Close(); err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return unix.Renameat(d.fd(), tmp, d.fd(), name)
}
func (d *privateDirectory) readlink(name string) (string, error) {
	buf := make([]byte, unix.PathMax)
	n, err := unix.Readlinkat(d.fd(), name, buf)
	if err != nil {
		return "", err
	}
	if n == len(buf) {
		return "", fmt.Errorf("guard symlink is too long")
	}
	return string(buf[:n]), nil
}
func (d *privateDirectory) link(name, target string) error {
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid guard link name")
	}
	if old, err := d.readlink(name); err == nil && old == target {
		return nil
	}
	tmp := ".link-" + rand.Text()
	if err := unix.Symlinkat(target, d.fd(), tmp); err != nil {
		return err
	}
	defer unix.Unlinkat(d.fd(), tmp, 0)
	return unix.Renameat(d.fd(), tmp, d.fd(), name)
}
func (d *privateDirectory) lock(exclusive bool) (func(), error) {
	fd, err := unix.Openat(d.fd(), "generation.lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "generation.lock")
	var s unix.Stat_t
	if err = unix.Fstat(fd, &s); err != nil || s.Mode&unix.S_IFMT != unix.S_IFREG || int(s.Uid) != os.Geteuid() || s.Mode&0077 != 0 {
		f.Close()
		return nil, fmt.Errorf("untrusted guard generation lock")
	}
	mode := unix.LOCK_SH
	if exclusive {
		mode = unix.LOCK_EX
	}
	if err = unix.Flock(fd, mode); err != nil {
		f.Close()
		return nil, err
	}
	return func() { unix.Flock(fd, unix.LOCK_UN); f.Close() }, nil
}
func generationName(name string) bool {
	return strings.HasPrefix(name, "generation-") && filepath.Base(name) == name
}

func (a *Assets) validateManaged(create bool) error {
	shell, err := a.privateShell(create)
	if err != nil {
		return err
	}
	shell.close()
	root, err := openPrivateState(a.Root, create)
	if err != nil {
		return err
	}
	defer root.close()
	guard, err := root.child("guard", create)
	if err != nil {
		return err
	}
	defer guard.close()
	markers, err := guard.child("agent-panes", create)
	if err != nil {
		return err
	}
	markers.close()
	bins, err := guard.child("bin", create)
	if err != nil {
		return err
	}
	defer bins.close()
	pane, err := bins.child(a.Pane, create)
	if err != nil {
		return err
	}
	pane.close()
	return nil
}
