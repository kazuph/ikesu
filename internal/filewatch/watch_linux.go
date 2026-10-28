package filewatch

import (
	"encoding/binary"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

type pathWatch struct {
	fd        int
	info      os.FileInfo
	listeners map[*Watcher]func()
}

// Inotify returns the same descriptor for aliases of an inode. Keep all path
// subscriptions until the final alias and subscriber have released that watch.
var changes struct {
	once  sync.Once
	mu    sync.Mutex
	fd    int
	err   error
	paths map[string]*pathWatch
	files map[int]map[*pathWatch]bool
	owned map[*Watcher]map[string]bool
}

func initChanges() {
	w := &changes
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fd, w.err = unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if w.err != nil {
		return
	}
	w.paths = map[string]*pathWatch{}
	w.files = map[int]map[*pathWatch]bool{}
	w.owned = map[*Watcher]map[string]bool{}
	file := os.NewFile(uintptr(w.fd), "shared-path-watch")
	go func() {
		buf := make([]byte, 64*(unix.SizeofInotifyEvent+unix.NAME_MAX+1))
		for {
			n, err := file.Read(buf)
			callbacks := map[*Watcher]func(){}
			w.mu.Lock()
			if err != nil {
				w.err = err
				for _, path := range w.paths {
					for a, fn := range path.listeners {
						callbacks[a] = fn
					}
				}
			} else {
				for off := 0; off+unix.SizeofInotifyEvent <= n; {
					wd := int(int32(binary.NativeEndian.Uint32(buf[off : off+4])))
					mask := binary.NativeEndian.Uint32(buf[off+4 : off+8])
					length := int(binary.NativeEndian.Uint32(buf[off+12 : off+16]))
					if mask&unix.IN_Q_OVERFLOW != 0 {
						for _, path := range w.paths {
							for a, fn := range path.listeners {
								callbacks[a] = fn
							}
						}
					} else {
						for path := range w.files[wd] {
							for a, fn := range path.listeners {
								callbacks[a] = fn
							}
							if mask&unix.IN_IGNORED != 0 {
								path.fd = -1
							}
						}
						if mask&unix.IN_IGNORED != 0 {
							delete(w.files, wd)
						}
					}
					off += unix.SizeofInotifyEvent + length
				}
			}
			w.mu.Unlock()
			for _, fn := range callbacks {
				fn()
			}
			if err != nil {
				file.Close()
				return
			}
		}
	}()
}

// Caller holds changes.mu.
func releasePath(path string) {
	w := &changes
	p := w.paths[path]
	delete(w.paths, path)
	if p.fd < 0 {
		return
	}
	delete(w.files[p.fd], p)
	if len(w.files[p.fd]) == 0 {
		unix.InotifyRmWatch(w.fd, uint32(p.fd))
		delete(w.files, p.fd)
	}
}

func removePath(path string, a *Watcher) {
	if p := changes.paths[path]; p != nil {
		delete(p.listeners, a)
		if len(p.listeners) == 0 {
			releasePath(path)
		}
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
		} // The supplied ancestor watches discover creation.
		if err != nil {
			complete = false
			continue
		}
		p := w.paths[path]
		listeners := map[*Watcher]func(){}
		if p != nil && (p.fd < 0 || !os.SameFile(info, p.info)) {
			listeners = p.listeners
			releasePath(path)
			p = nil
			defer func() {
				for _, fn := range listeners {
					fn()
				}
			}()
		}
		if p == nil {
			fd, err := unix.InotifyAddWatch(w.fd, path, unix.IN_MODIFY|unix.IN_ATTRIB|unix.IN_CREATE|unix.IN_DELETE|unix.IN_MOVED_FROM|unix.IN_MOVED_TO|unix.IN_DELETE_SELF|unix.IN_MOVE_SELF)
			if err != nil {
				complete = false
				continue
			}
			p = &pathWatch{fd: fd, info: info, listeners: listeners}
			w.paths[path] = p
			if w.files[fd] == nil {
				w.files[fd] = map[*pathWatch]bool{}
			}
			w.files[fd][p] = true
		}
		p.listeners[a] = changed
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
	w.mu.Lock()
	defer w.mu.Unlock()
	for path := range w.owned[a] {
		removePath(path, a)
	}
	delete(w.owned, a)
}
