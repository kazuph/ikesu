package dispatch

import (
	"os"
	"syscall"
)

// WithJobLogLock serializes late diagnostic publication with deletion of the
// parent log. Opening never creates it: a retired job cannot reappear.
func WithJobLogLock(path string, work func() error) error {
	_, err := withJobLogLock(path, syscall.LOCK_EX, work)
	return err
}

// TryWithJobLogLock lets completion continue when a diagnostic publisher is
// paused while owning the log. Recovery reports the unavailable diagnostic.
func TryWithJobLogLock(path string, work func() error) error {
	_, err := withJobLogLock(path, syscall.LOCK_EX|syscall.LOCK_NB, work)
	return err
}

// opened distinguishes a missing parent from loss of ownership after opening.
func withJobLogLock(path string, mode int, work func() error) (opened bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), mode); err != nil {
		return true, err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	before, err := f.Stat()
	if err != nil {
		return true, err
	}
	after, err := os.Stat(path)
	if err != nil {
		return true, err
	}
	if !os.SameFile(before, after) {
		return true, os.ErrNotExist
	}
	return true, work()
}

var jobFileSuffixes = []string{"", ".runner", ".result", ".cancel", ".monitor-stderr.log", ".monitor-exit.json", ".completion-unavailable.json"}
