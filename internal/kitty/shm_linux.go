package kitty

import (
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func shmOpen(name string, flags int, mode uint32) (int, error) {
	return unix.Open("/dev/shm/"+strings.TrimPrefix(name, "/"), flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, mode)
}

func shmUnlink(name string) error {
	return os.Remove("/dev/shm/" + strings.TrimPrefix(name, "/"))
}
