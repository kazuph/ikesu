package server

import "golang.org/x/sys/unix"

func waitPeerHangup(fd, stop int) {
	kq, err := unix.Kqueue()
	if err != nil {
		return
	}
	defer unix.Close(kq)
	changes := make([]unix.Kevent_t, 2)
	unix.SetKevent(&changes[0], fd, unix.EVFILT_WRITE, unix.EV_ADD|unix.EV_CLEAR)
	unix.SetKevent(&changes[1], stop, unix.EVFILT_READ, unix.EV_ADD|unix.EV_CLEAR)
	events := make([]unix.Kevent_t, 2)
	for {
		n, err := unix.Kevent(kq, changes, events, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return
		}
		changes = nil
		for _, event := range events[:n] {
			if int(event.Ident) == stop || event.Flags&(unix.EV_EOF|unix.EV_ERROR) != 0 {
				return
			}
		}
	}
}
