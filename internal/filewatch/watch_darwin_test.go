//go:build darwin

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
