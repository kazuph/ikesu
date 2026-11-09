package server

import "golang.org/x/sys/unix"

func newPeerEventQueue() (int, error) { return unix.Kqueue() }

func addPeerEvent(queue, fd int, read bool) error {
	var event unix.Kevent_t
	filter := unix.EVFILT_WRITE
	if read {
		filter = unix.EVFILT_READ
	}
	unix.SetKevent(&event, fd, filter, unix.EV_ADD|unix.EV_CLEAR)
	_, err := unix.Kevent(queue, []unix.Kevent_t{event}, nil, nil)
	return err
}

func deletePeerEvent(queue, fd int) {
	var event unix.Kevent_t
	unix.SetKevent(&event, fd, unix.EVFILT_WRITE, unix.EV_DELETE)
	_, _ = unix.Kevent(queue, []unix.Kevent_t{event}, nil, nil)
}

func waitPeerEvents(queue int) ([]int, error) {
	var events [128]unix.Kevent_t
	n, err := unix.Kevent(queue, nil, events[:], nil)
	if err != nil {
		return nil, err
	}
	ready := make([]int, 0, n)
	for _, e := range events[:n] {
		if e.Filter == unix.EVFILT_READ || e.Flags&(unix.EV_EOF|unix.EV_ERROR) != 0 {
			ready = append(ready, int(e.Ident))
		}
	}
	return ready, nil
}
