//go:build darwin

package runtime

import (
	"golang.org/x/sys/unix"
	"sync"
)

// One kernel wait serves every pane. Quiet panes have no timer, syscall, or
// blocked OS thread of their own. The PTY stream covers shell cwd changes.
var processChanges struct {
	once  sync.Once
	mu    sync.Mutex
	fd    int
	err   error
	panes map[int]map[*Pane]bool
	owned map[*Pane]map[int]bool
}

func processEventQueue() bool {
	w := &processChanges
	w.once.Do(func() {
		w.fd, w.err = unix.Kqueue()
		if w.err != nil {
			return
		}
		unix.CloseOnExec(w.fd)
		w.panes = make(map[int]map[*Pane]bool)
		w.owned = make(map[*Pane]map[int]bool)
		go func() {
			events := make([]unix.Kevent_t, 64)
			for {
				n, err := unix.Kevent(w.fd, nil, events, nil)
				if err == unix.EINTR {
					continue
				}
				if err != nil {
					return
				}
				w.mu.Lock()
				for _, ev := range events[:n] {
					for p := range w.panes[int(ev.Ident)] {
						p.markProcessChanged()
						p.notifyChanged()
						if ev.Fflags&unix.NOTE_EXIT != 0 {
							delete(w.owned[p], int(ev.Ident))
							if len(w.owned[p]) == 0 {
								delete(w.owned, p)
							}
						}
					}
					if ev.Fflags&unix.NOTE_EXIT != 0 {
						delete(w.panes, int(ev.Ident))
					}
				}
				w.mu.Unlock()
			}
		}()
	})
	return w.err == nil
}

func (p *Pane) watchProcessChanges(group int) {
	p.mu.Lock()
	dead := p.dead
	p.mu.Unlock()
	if dead || p.retired.Load() {
		return
	}
	if !processEventQueue() {
		return
	}
	w := &processChanges
	w.mu.Lock()
	defer w.mu.Unlock()
	shell := p.ShellPID()
	for pid := range w.owned[p] {
		if pid != shell && pid != group {
			removeProcessWatch(pid, p)
		}
	}
	for _, pid := range []int{shell, group} {
		if pid <= 0 {
			continue
		}
		panes := w.panes[pid]
		if panes[p] {
			continue
		}
		if panes == nil {
			event := unix.Kevent_t{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_CLEAR, Fflags: unix.NOTE_FORK | unix.NOTE_EXEC | unix.NOTE_EXIT}
			if _, err := unix.Kevent(w.fd, []unix.Kevent_t{event}, nil, nil); err != nil {
				continue
			}
			panes = make(map[*Pane]bool)
			w.panes[pid] = panes
		}
		panes[p] = true
		if w.owned[p] == nil {
			w.owned[p] = make(map[int]bool)
		}
		w.owned[p][pid] = true
	}
}

// Caller holds processChanges.mu.
func removeProcessWatch(pid int, p *Pane) {
	w := &processChanges
	panes := w.panes[pid]
	delete(panes, p)
	delete(w.owned[p], pid)
	if len(w.owned[p]) == 0 {
		delete(w.owned, p)
	}
	if len(panes) == 0 {
		delete(w.panes, pid)
		event := unix.Kevent_t{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_DELETE}
		_, _ = unix.Kevent(w.fd, []unix.Kevent_t{event}, nil, nil)
	}
}

func (p *Pane) beginProcessWatch() {
	p.processChanged.Store(true)
	p.watchProcessChanges(0)
}

func (p *Pane) endProcessWatch() {
	if !processEventQueue() {
		return
	}
	w := &processChanges
	w.mu.Lock()
	defer w.mu.Unlock()
	for pid := range w.owned[p] {
		removeProcessWatch(pid, p)
	}
}
