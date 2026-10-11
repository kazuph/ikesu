package client

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

// Watch the catalog inode, not session directory writes during pane output.
// Atomic catalog replacement rearms the watch on the replacement inode.
func watchMachineCatalog(parent context.Context, path string, changed func(context.Context, error)) (func(), error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(kq)
	fd := -1
	directory := false
	install := func() error {
		if fd >= 0 {
			unix.Close(fd)
			fd = -1
		}
		directory = false
		fd, err = unix.Open(path, unix.O_EVTONLY|unix.O_CLOEXEC, 0)
		if os.IsNotExist(err) {
			directory = true
			fd, err = unix.Open(filepath.Dir(path), unix.O_EVTONLY|unix.O_CLOEXEC, 0)
		}
		if err != nil {
			return err
		}
		_, err = unix.Kevent(kq, []unix.Kevent_t{{Ident: uint64(fd), Filter: unix.EVFILT_VNODE, Flags: unix.EV_ADD | unix.EV_CLEAR, Fflags: unix.NOTE_WRITE | unix.NOTE_DELETE | unix.NOTE_RENAME}}, nil, nil)
		return err
	}
	if err = install(); err == nil {
		_, err = unix.Kevent(kq, []unix.Kevent_t{{Ident: uint64(kq), Filter: unix.EVFILT_USER, Flags: unix.EV_ADD | unix.EV_CLEAR}}, nil, nil)
	}
	if err != nil {
		if fd >= 0 {
			unix.Close(fd)
		}
		unix.Close(kq)
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			unix.Kevent(kq, []unix.Kevent_t{{Ident: uint64(kq), Filter: unix.EVFILT_USER, Fflags: unix.NOTE_TRIGGER}}, nil, nil)
			<-done
		})
	}
	go func() {
		defer close(done)
		defer unix.Close(kq)
		defer func() { unix.Close(fd) }()
		events := make([]unix.Kevent_t, 1)
		for {
			_, err := unix.Kevent(kq, nil, events, nil)
			if err == unix.EINTR {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				changed(ctx, err)
				return
			}
			event := events[0]
			if event.Filter != unix.EVFILT_VNODE {
				continue
			}
			if directory {
				if _, err := os.Stat(path); os.IsNotExist(err) {
					continue
				}
			}
			if directory || event.Fflags&(unix.NOTE_DELETE|unix.NOTE_RENAME) != 0 {
				if err := install(); err != nil {
					changed(ctx, err)
					return
				}
			}
			changed(ctx, nil)
		}
	}()
	return stop, nil
}
