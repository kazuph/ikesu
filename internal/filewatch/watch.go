// Package filewatch shares native path subscriptions between change-driven
// integrity checks and metadata readers. Unsupported/failed watches report false.
package filewatch

import (
	"sync"
	"sync/atomic"
)

type Watcher struct {
	mu       sync.Mutex
	closed   bool
	changed  func()
	revision atomic.Uint64
}

func New(changed func()) *Watcher {
	w := &Watcher{}
	w.changed = func() {
		w.revision.Add(1)
		if changed != nil {
			changed()
		}
	}
	return w
}

func (w *Watcher) Revision() uint64 { return w.revision.Load() }

func (w *Watcher) Update(paths []string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return false
	}
	return watchPaths(w, paths, w.changed)
}

func (w *Watcher) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	unwatchPaths(w)
}
