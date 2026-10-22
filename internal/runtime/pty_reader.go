package runtime

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// A blocking PTY stays out of Go's edge-triggered poller: continuous output
// otherwise generates kqueue wakeups while the reader is already draining it.
// A private pipe interrupts the readiness wait without consuming pane output.
func (p *Pane) installPTY(original *os.File) error {
	fd, err := unix.FcntlInt(original.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return err
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		unix.Close(fd)
		return err
	}
	original.Close()
	file := os.NewFile(uintptr(fd), "ikesu-pty")
	read, write, err := os.Pipe()
	if err != nil {
		file.Close()
		return err
	}
	p.ptyFile, p.ptyFD = file, fd
	p.wakeRead, p.wakeWrite, p.wakeFD = read, write, int(read.Fd())
	return nil
}

func (p *Pane) readPTYReady(buf []byte) (int, error) {
	if p.wakeRead == nil {
		return p.ptyFile.Read(buf)
	}
	fds := []unix.PollFd{{Fd: int32(p.ptyFD), Events: unix.POLLIN}, {Fd: int32(p.wakeFD), Events: unix.POLLIN}}
	for {
		_, err := unix.Poll(fds, -1)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		if fds[1].Revents != 0 || p.pausing.Load() {
			return 0, io.EOF
		}
		if fds[0].Revents != 0 {
			return p.ptyFile.Read(buf)
		}
	}
}

func (p *Pane) closePTY() {
	if p.wakeWrite != nil {
		p.wakeWrite.Close()
	}
	if p.ptyFile != nil {
		p.ptyFile.Close()
	}
	// The pump's readiness wait must finish before its pipe descriptor is closed
	// and can be reused by another pane.
	if p.pumpDone != nil {
		done, read := p.pumpDone, p.wakeRead
		go func() { <-done; read.Close() }()
	} else if p.wakeRead != nil {
		p.wakeRead.Close()
	}
}
