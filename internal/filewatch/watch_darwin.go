//go:build darwin

package filewatch

import (
	"golang.org/x/sys/unix"
	"os"
	"sync"
)

type pathWatch struct {
	fd        int
	info      os.FileInfo
	listeners map[*Watcher]func()
}

// One kernel wait and one descriptor per distinct path serve all guarded panes.
var changes struct {
	once  sync.Once
	mu    sync.Mutex
	fd    int
	err   error
	paths map[string]*pathWatch
	files map[int]*pathWatch
	owned map[*Watcher]map[string]bool
}

func initChanges() {
	w := &changes
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fd, w.err = unix.Kqueue()
	if w.err != nil {
		return
	}
	unix.CloseOnExec(w.fd)
	w.paths = map[string]*pathWatch{}
	w.files = map[int]*pathWatch{}
	w.owned = map[*Watcher]map[string]bool{}
	go func() {
		events := make([]unix.Kevent_t, 64)
		for {
			n, err := unix.Kevent(w.fd, nil, events, nil)
			if err == unix.EINTR {
				continue
			}
			callbacks := map[*Watcher]func(){}
			w.mu.Lock()
			if err != nil {
				w.err = err
				for _, file := range w.paths {
					for a, changed := range file.listeners {
						callbacks[a] = changed
					}
				}
			} else {
				for _, ev := range events[:n] {
					if file := w.files[int(ev.Ident)]; file != nil {
						for a, changed := range file.listeners {
							callbacks[a] = changed
						}
					}
				}
			}
			w.mu.Unlock()
			for _, changed := range callbacks {
				changed()
			}
			if err != nil {
				return
			}
		}
	}()
}

// Caller holds changes.mu.
func removePath(path string, a *Watcher) {
	w := &changes
	file := w.paths[path]
	if file == nil {
		return
	}
	delete(file.listeners, a)
	if len(file.listeners) == 0 {
		delete(w.paths, path)
		delete(w.files, file.fd)
		unix.Close(file.fd)
	}
}

func watchPaths(a *Watcher, paths []string, changed func()) bool {
	w := &changes
	w.once.Do(initChanges)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return false
	}
	wanted := map[string]bool{}
	complete := true
	for _, path := range paths {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		} // Its nearest existing ancestor is watched.
		if err != nil {
			complete = false
			continue
		}
		file := w.paths[path]
		listeners := map[*Watcher]func(){}
		if file != nil && !os.SameFile(info, file.info) {
			// Every subscriber needs the replacement inode, including other panes.
			listeners = file.listeners
			delete(w.files, file.fd)
			unix.Close(file.fd)
			delete(w.paths, path)
			file = nil
			defer func() {
				for _, notify := range listeners {
					notify()
				}
			}()
		}
		if file == nil {
			fd, err := unix.Open(path, unix.O_EVTONLY|unix.O_CLOEXEC, 0)
			if err != nil {
				complete = false
				continue
			}
			ev := unix.Kevent_t{Ident: uint64(fd), Filter: unix.EVFILT_VNODE, Flags: unix.EV_ADD | unix.EV_CLEAR, Fflags: unix.NOTE_WRITE | unix.NOTE_DELETE | unix.NOTE_RENAME | unix.NOTE_ATTRIB | unix.NOTE_REVOKE}
			if _, err = unix.Kevent(w.fd, []unix.Kevent_t{ev}, nil, nil); err != nil {
				unix.Close(fd)
				complete = false
				continue
			}
			file = &pathWatch{fd: fd, info: info, listeners: listeners}
			w.paths[path] = file
			w.files[fd] = file
		}
		file.listeners[a] = changed
		wanted[path] = true
	}
	for path := range w.owned[a] {
		if !wanted[path] {
			removePath(path, a)
		}
	}
	w.owned[a] = wanted
	return complete && len(wanted) > 0
}

func unwatchPaths(a *Watcher) {
	w := &changes
	// Do not initialize the watcher just to close an unwatched Assets.
	w.mu.Lock()
	defer w.mu.Unlock()
	for path := range w.owned[a] {
		removePath(path, a)
	}
	delete(w.owned, a)
}
