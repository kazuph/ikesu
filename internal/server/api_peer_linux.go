package server

import "golang.org/x/sys/unix"

func waitPeerHangup(fd, stop int) {
	ep, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return
	}
	defer unix.Close(ep)
	if unix.EpollCtl(ep, unix.EPOLL_CTL_ADD, fd, &unix.EpollEvent{Fd: int32(fd)}) != nil {
		return
	}
	if unix.EpollCtl(ep, unix.EPOLL_CTL_ADD, stop, &unix.EpollEvent{Events: unix.EPOLLIN, Fd: int32(stop)}) != nil {
		return
	}
	events := make([]unix.EpollEvent, 2)
	for {
		n, err := unix.EpollWait(ep, events, -1)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return
		}
		for _, event := range events[:n] {
			if int(event.Fd) == stop || event.Events&(unix.EPOLLHUP|unix.EPOLLERR) != 0 {
				return
			}
		}
	}
}
