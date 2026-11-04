//go:build darwin

package runtime

import (
	"bytes"
	"fmt"
	"unsafe"

	"github.com/kazuph/ikesu/internal/guard"
	"golang.org/x/sys/unix"
)

// A legacy handoff has a PTY master and numeric PID, but no birth time. The
// kernel's controlling-terminal device and session leader bind that live PID
// to this transferred PTY before recording its generation.
func legacyShellStart(fd, pid int) (string, error) {
	parent, start, err := guard.ApprovalProcess(pid)
	if err != nil {
		return "", err
	}
	sid, err := unix.Getsid(pid)
	if err != nil || sid != pid {
		return "", fmt.Errorf("pane is not its terminal session leader")
	}
	var name [128]byte
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0])))
	if errno != 0 {
		return "", errno
	}
	end := bytes.IndexByte(name[:], 0)
	if end <= 0 {
		return "", fmt.Errorf("invalid terminal name")
	}
	var tty unix.Stat_t
	if err = unix.Stat(string(name[:end]), &tty); err != nil {
		return "", err
	}
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || proc.Proc.P_pid != int32(pid) || proc.Eproc.Tdev != tty.Rdev {
		return "", fmt.Errorf("pane terminal does not match process")
	}
	parent2, start2, err := guard.ApprovalProcess(pid)
	if err != nil || start2 != start || parent2 != parent {
		return "", fmt.Errorf("pane identity changed")
	}
	return start, nil
}
