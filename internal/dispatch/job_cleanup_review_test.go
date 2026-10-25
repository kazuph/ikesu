package dispatch

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestCleanupDefersLockedJobAndItsEntireFamily(t *testing.T) {
	s := testStore(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "expired.log")
	now := time.Now().UTC()
	for _, suffix := range jobFileSuffixes {
		if e := os.WriteFile(log+suffix, []byte("owned"), 0600); e != nil {
			t.Fatal(e)
		}
		os.Chtimes(log+suffix, now.Add(-2*JobRetention), now.Add(-2*JobRetention))
	}
	if e := s.InsertCommand("expired", "expired", "true", dir, "p1", "owner", "none", log); e != nil {
		t.Fatal(e)
	}
	if e := s.MarkCommandRunning("expired", int64(os.Getpid())); e != nil {
		t.Fatal(e)
	}
	if _, e := s.MarkCommandFinished("expired", nil); e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(log)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); e != nil {
		t.Fatal(e)
	}
	locked := true
	defer func() {
		if locked {
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		}
	}()
	done := make(chan error, 1)
	go func() { done <- s.CleanupJobs(now.Add(2*JobRetention), dir, func(int) bool { return false }) }()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		locked = false
		<-done
		t.Fatal("cleanup blocked behind diagnostic flock")
	}
	for _, suffix := range jobFileSuffixes {
		if _, e := os.Stat(log + suffix); e != nil {
			t.Fatal("busy family deleted", suffix, e)
		}
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	locked = false
	if e = s.CleanupJobs(now.Add(2*JobRetention), dir, func(int) bool { return false }); e != nil {
		t.Fatal(e)
	}
	for _, suffix := range jobFileSuffixes {
		if _, e := os.Stat(log + suffix); !os.IsNotExist(e) {
			t.Fatal("deferred family not removed", suffix, e)
		}
	}
}

func TestCleanupWaitingOnOldInodePreservesReplacement(t *testing.T) {
	log := filepath.Join(t.TempDir(), "job.log")
	if e := os.WriteFile(log, []byte("old"), 0600); e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(log)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); e != nil {
		t.Fatal(e)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	old, e := f.Stat()
	if e != nil {
		t.Fatal(e)
	}
	count := func() int {
		entries, _ := os.ReadDir("/dev/fd")
		n := 0
		for _, entry := range entries {
			fd, e := strconv.Atoi(entry.Name())
			var info syscall.Stat_t
			if e == nil {
				e = syscall.Fstat(fd, &info)
			}
			st := old.Sys().(*syscall.Stat_t)
			if e == nil && info.Dev == st.Dev && info.Ino == st.Ino {
				n++
			}
		}
		return n
	}
	before := count()
	done := make(chan error, 1)
	go func() { done <- removeJobFiles(log) }()
	// The old implementation blocks after opening the old inode; the fixed
	// implementation returns without waiting. Either event fixes the ordering.
	returned := false
	deadline := time.After(5 * time.Second)
	for !returned && count() == before {
		select {
		case e := <-done:
			if e != nil && !errors.Is(e, errJobCleanupDeferred) {
				t.Fatal(e)
			}
			returned = true
		case <-deadline:
			t.Fatal("cleanup neither opened nor returned")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if e = os.Remove(log); e != nil {
		t.Fatal(e)
	}
	for _, suffix := range []string{"", ".monitor-exit.json"} {
		if e = os.WriteFile(log+suffix, []byte("new"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if !returned {
		select {
		case e := <-done:
			if e != nil && !errors.Is(e, errJobCleanupDeferred) {
				t.Fatal(e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cleanup did not return")
		}
	}
	for _, suffix := range []string{"", ".monitor-exit.json"} {
		b, e := os.ReadFile(log + suffix)
		if e != nil || string(b) != "new" {
			t.Fatal("replacement removed", suffix, string(b), e)
		}
	}
}
