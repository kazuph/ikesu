package cli

import "golang.org/x/sys/unix"

func outputPipeClosed(fd, wake int) bool {
	fds := []unix.PollFd{{Fd: int32(fd)}, {Fd: int32(wake), Events: unix.POLLIN}}
	for {
		_, err := unix.Poll(fds, -1)
		if err == unix.EINTR {
			continue
		}
		return err == nil && fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0
	}
}
