package cli

import "golang.org/x/sys/unix"

// kqueue reports EV_EOF when a pipe reader closes, including an idle stream.
func outputPipeClosed(fd, wake int) bool {
	kq, err := unix.Kqueue()
	if err != nil {
		return false
	}
	defer unix.Close(kq)
	changes := []unix.Kevent_t{
		{Ident: uint64(fd), Filter: unix.EVFILT_WRITE, Flags: unix.EV_ADD | unix.EV_CLEAR},
		{Ident: uint64(wake), Filter: unix.EVFILT_READ, Flags: unix.EV_ADD | unix.EV_CLEAR},
	}
	events := make([]unix.Kevent_t, 2)
	for {
		n, err := unix.Kevent(kq, changes, events, nil)
		changes = nil
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false
		}
		for _, event := range events[:n] {
			if event.Ident == uint64(wake) {
				return false
			}
			if event.Flags&unix.EV_EOF != 0 {
				return true
			}
		}
	}
}
