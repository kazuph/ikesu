//go:build darwin || linux

package filewatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedPathWatchesNotifyBothAndReleaseOwnership(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tracked")
	if err := os.WriteFile(path, []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	a, b := New(nil), New(nil)
	defer a.Close()
	defer b.Close()
	for _, w := range []*Watcher{a, b} {
		if !w.Update([]string{root, path}) {
			t.Fatal("native path watch unavailable")
		}
	}
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(5 * time.Second)
	for a.Revision() == 0 || b.Revision() == 0 {
		if time.Now().After(until) {
			t.Fatal("shared subscribers lost a file change", a.Revision(), b.Revision())
		}
		time.Sleep(time.Millisecond)
	}
	a.Close()
	changes.mu.Lock()
	_, owned := changes.owned[a]
	if file := changes.paths[path]; file == nil || file.listeners[b] == nil || file.listeners[a] != nil {
		t.Error("closing one subscriber changed the other subscription")
	}
	changes.mu.Unlock()
	if owned {
		t.Fatal("closed watcher retained ownership")
	}
	b.Close()
	changes.mu.Lock()
	_, owned = changes.owned[b]
	_, retained := changes.paths[path]
	changes.mu.Unlock()
	if owned || retained {
		t.Fatal("last subscriber did not release file watches")
	}
}

func TestPathWatchReattachesReplacementAndDiscoversMissingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tracked")
	w := New(nil)
	defer w.Close()
	wait := func(before uint64) {
		t.Helper()
		until := time.Now().Add(5 * time.Second)
		for w.Revision() == before {
			if time.Now().After(until) {
				t.Fatal("file change was not delivered")
			}
			time.Sleep(time.Millisecond)
		}
	}
	if !w.Update([]string{root, path}) {
		t.Fatal("ancestor watch unavailable")
	}
	before := w.Revision()
	if err := os.WriteFile(path, []byte("created"), 0600); err != nil {
		t.Fatal(err)
	}
	wait(before)
	if !w.Update([]string{root, path}) {
		t.Fatal("created file watch unavailable")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	if !w.Update([]string{path}) {
		t.Fatal("replacement file watch unavailable")
	}
	changes.mu.Lock()
	file := changes.paths[path]
	info, err := os.Stat(path)
	attached := file != nil && file.fd >= 0 && err == nil && os.SameFile(file.info, info)
	changes.mu.Unlock()
	if !attached {
		t.Fatal("watch still refers to the removed inode")
	}
	changed := make(chan struct{}, 1)
	b := New(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	defer b.Close()
	if !b.Update([]string{path}) {
		t.Fatal("replacement subscription unavailable")
	}
	if err := os.WriteFile(path, []byte("after replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("replacement lost notifications")
	}
}
