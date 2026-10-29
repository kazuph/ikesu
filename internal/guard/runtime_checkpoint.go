package guard

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

type runtimeFile struct {
	kind   os.FileMode
	mode   os.FileMode
	data   []byte
	target string
	absent bool
}
type RuntimeCheckpoint struct {
	state    Transfer
	files    map[string]runtimeFile
	marker   runtimeFile
	injected bool
	bins     map[string]runtimeFile
	issue    runtimeFile
}

var stableRuntimeNames = []string{"claude-prefix", "bash-env", "bash-env.current", ".zshenv", ".zshenv.current", ".zprofile", ".zprofile.current", ".zshrc", ".zshrc.current", ".zlogin", ".zlogin.current", "ikesu", "current"}

func captureRuntimeFile(dir *privateDirectory, name string) (runtimeFile, error) {
	var st unix.Stat_t
	err := unix.Fstatat(dir.fd(), name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if err == unix.ENOENT {
		return runtimeFile{absent: true}, nil
	}
	if err != nil {
		return runtimeFile{}, err
	}
	if int(st.Uid) != os.Geteuid() {
		return runtimeFile{}, fmt.Errorf("guard checkpoint has foreign owner")
	}
	f := runtimeFile{mode: os.FileMode(st.Mode & 0777)}
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFLNK:
		f.kind = os.ModeSymlink
		f.target, err = dir.readlink(name)
	case unix.S_IFREG:
		f.data, err = dir.read(name)
	case unix.S_IFDIR:
		f.kind = os.ModeDir
	default:
		err = fmt.Errorf("unsupported guard checkpoint file")
	}
	return f, err
}
func restoreRuntimeFile(dir *privateDirectory, name string, f runtimeFile) error {
	if f.absent {
		err := unix.Unlinkat(dir.fd(), name, 0)
		if err == unix.ENOENT {
			return nil
		}
		return err
	}
	if f.kind == os.ModeSymlink {
		return dir.link(name, f.target)
	}
	if f.kind == os.ModeDir {
		var st unix.Stat_t
		if err := unix.Fstatat(dir.fd(), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR {
			return fmt.Errorf("checkpoint directory changed")
		}
		return nil
	}
	return dir.write(name, f.data, f.mode)
}
func (a *Assets) markerDirectory() (*privateDirectory, error) {
	root, err := openPrivateState(a.Root, false)
	if err != nil {
		return nil, err
	}
	defer root.close()
	guard, err := root.child("guard", false)
	if err != nil {
		return nil, err
	}
	defer guard.close()
	return guard.child("agent-panes", false)
}
func (a *Assets) checkpointLocked() (*RuntimeCheckpoint, error) {
	c := &RuntimeCheckpoint{state: Transfer{Root: a.Root, Pane: a.Pane, Bin: a.Bin, MarkerPath: a.MarkerPath, Executable: a.Executable, Active: a.active, Config: a.Config, Meta: a.meta, Injected: a.injected, Prompt: a.prompt, Mode: a.mode, Unsupported: a.unsupported, Issue: a.issue, Native: a.native}, files: map[string]runtimeFile{}, bins: map[string]runtimeFile{}, injected: a.injected && a.Config.Active()}
	if !c.injected {
		return c, nil
	}
	shell, err := a.privateShell(false)
	if err != nil {
		return nil, err
	}
	defer shell.close()
	unlock, err := shell.lock(true)
	if err != nil {
		return nil, err
	}
	defer unlock()
	for _, name := range stableRuntimeNames {
		file, err := captureRuntimeFile(shell, name)
		if err != nil {
			return nil, err
		}
		c.files[name] = file
	}
	markers, err := a.markerDirectory()
	if err != nil {
		return nil, err
	}
	defer markers.close()
	c.marker, err = captureRuntimeFile(markers, filepath.Base(a.MarkerPath))
	if err != nil {
		return nil, err
	}
	bin, err := a.privateBin(false)
	if err != nil {
		return nil, err
	}
	defer bin.close()
	for _, name := range a.Config.Commands() {
		f, e := captureRuntimeFile(bin, name)
		if e != nil {
			return nil, e
		}
		c.bins[name] = f
	}
	issue, err := a.privateIssue(false)
	if os.IsNotExist(err) {
		c.issue = runtimeFile{absent: true}
		err = nil
	} else if err != nil {
		return nil, err
	} else {
		defer issue.close()
		c.issue, err = captureRuntimeFile(issue, filepath.Base(a.issuePath()))
	}
	return c, err
}
func (a *Assets) Checkpoint() (*RuntimeCheckpoint, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.checkpointLocked()
}
func (a *Assets) restoreCheckpointLocked(c *RuntimeCheckpoint) error {
	if c.injected {
		shell, err := a.privateShell(false)
		if err != nil {
			return err
		}
		defer shell.close()
		unlock, err := shell.lock(true)
		if err != nil {
			return err
		}
		defer unlock()
		for _, name := range stableRuntimeNames {
			if name == "current" {
				continue
			}
			if err = restoreRuntimeFile(shell, name, c.files[name]); err != nil {
				return err
			}
		}
		markers, err := a.markerDirectory()
		if err != nil {
			return err
		}
		defer markers.close()
		if err = restoreRuntimeFile(markers, filepath.Base(a.MarkerPath), c.marker); err != nil {
			return err
		}
		bin, err := a.privateBin(false)
		if err != nil {
			return err
		}
		defer bin.close()
		for name, f := range c.bins {
			if err = restoreRuntimeFile(bin, name, f); err != nil {
				return err
			}
		}
		issue, err := a.privateIssue(!c.issue.absent)
		if err == nil {
			defer issue.close()
			if err = restoreRuntimeFile(issue, filepath.Base(a.issuePath()), c.issue); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err = restoreRuntimeFile(shell, "current", c.files["current"]); err != nil {
			return err
		}
	}
	a.native = c.state.Native
	a.Executable, a.active, a.Config, a.meta = c.state.Executable, c.state.Active, c.state.Config, c.state.Meta
	a.injected, a.prompt, a.mode, a.unsupported, a.issue = c.state.Injected, c.state.Prompt, c.state.Mode, c.state.Unsupported, c.state.Issue
	return nil
}
func (a *Assets) RestoreCheckpoint(c *RuntimeCheckpoint) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	defer a.refreshChanges()
	return a.restoreCheckpointLocked(c)
}

func (a *Assets) privateBin(create bool) (*privateDirectory, error) {
	root, err := openPrivateState(a.Root, create)
	if err != nil {
		return nil, err
	}
	defer root.close()
	guard, err := root.child("guard", create)
	if err != nil {
		return nil, err
	}
	defer guard.close()
	bins, err := guard.child("bin", create)
	if err != nil {
		return nil, err
	}
	defer bins.close()
	return bins.child(a.Pane, create)
}
func (a *Assets) privateIssue(create bool) (*privateDirectory, error) {
	root, err := openPrivateState(a.Root, create)
	if err != nil {
		return nil, err
	}
	defer root.close()
	guard, err := root.child("guard", create)
	if err != nil {
		return nil, err
	}
	defer guard.close()
	return guard.child("uncovered", create)
}
