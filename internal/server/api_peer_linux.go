package server

import "golang.org/x/sys/unix"

func newPeerEventQueue() (int, error) { return unix.EpollCreate1(unix.EPOLL_CLOEXEC) }

func addPeerEvent(queue, fd int, read bool) error {
	var flags uint32
	if read {
		flags = unix.EPOLLIN
	}
	return unix.EpollCtl(queue, unix.EPOLL_CTL_ADD, fd, &unix.EpollEvent{Events: flags, Fd: int32(fd)})
}

func deletePeerEvent(queue, fd int) { _ = unix.EpollCtl(queue, unix.EPOLL_CTL_DEL, fd, nil) }

func waitPeerEvents(queue int) ([]int, error) {
	var events [128]unix.EpollEvent
	n, err := unix.EpollWait(queue, events[:], -1)
	if err != nil {
		return nil, err
	}
	ready := make([]int, 0, n)
	for _, e := range events[:n] {
		if e.Events&(unix.EPOLLIN|unix.EPOLLHUP|unix.EPOLLERR) != 0 {
			ready = append(ready, int(e.Fd))
		}
	}
	return ready, nil
}
