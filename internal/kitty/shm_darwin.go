package kitty

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

func shmOpen(name string, flags int, mode uint32) (int, error) {
	pointer, err := unix.BytePtrFromString(name)
	if err != nil {
		return -1, err
	}
	fd, _, errno := unix.Syscall(unix.SYS_SHM_OPEN, uintptr(unsafe.Pointer(pointer)), uintptr(flags), uintptr(mode))
	if errno != 0 {
		return -1, errno
	}
	unix.CloseOnExec(int(fd))
	return int(fd), nil
}

func shmUnlink(name string) error {
	pointer, err := unix.BytePtrFromString(name)
	if err != nil {
		return err
	}
	_, _, errno := unix.Syscall(unix.SYS_SHM_UNLINK, uintptr(unsafe.Pointer(pointer)), 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
