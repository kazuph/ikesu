//go:build linux

package runtime

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kazuph/ikesu/internal/guard"
	"golang.org/x/sys/unix"
)

func legacyShellStart(fd, pid int) (string, error) {
	parent, start, err := guard.ApprovalProcess(pid)
	if err != nil {
		return "", err
	}
	sid, err := unix.Getsid(pid)
	if err != nil || sid != pid {
		return "", fmt.Errorf("pane is not its terminal session leader")
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		return "", err
	}
	var tty unix.Stat_t
	if err = unix.Stat("/dev/pts/"+strconv.Itoa(number), &tty); err != nil {
		return "", err
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 5 {
		return "", fmt.Errorf("missing process terminal")
	}
	device, err := strconv.ParseInt(fields[4], 10, 64)
	if err != nil || uint64(uint32(device)) != tty.Rdev {
		return "", fmt.Errorf("pane terminal does not match process")
	}
	parent2, start2, err := guard.ApprovalProcess(pid)
	if err != nil || start2 != start || parent2 != parent {
		return "", fmt.Errorf("pane identity changed")
	}
	return start, nil
}
