package kitty

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

var errImageFile = errors.New("EBADF:Failed to read image file")

func mediaPayload(keys map[string]string, payload []byte) ([]byte, error) {
	medium := keys["t"]
	if medium == "" || medium == "d" {
		return payload, nil
	}
	if medium == "s" {
		return sharedMemoryPayload(keys, string(payload))
	}
	if medium != "f" && medium != "t" {
		return nil, errors.New("ENOTSUP:unsupported image medium")
	}
	path, err := filepath.EvalSymlinks(string(payload))
	if err != nil {
		return nil, errImageFile
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, errImageFile
	}
	for _, sensitive := range []string{"/dev", "/proc", "/sys"} {
		if path == sensitive || strings.HasPrefix(path, sensitive+"/") {
			return nil, errImageFile
		}
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errImageFile
	}
	if medium == "t" {
		temporary, err := filepath.EvalSymlinks(os.TempDir())
		if err != nil || !strings.HasPrefix(path, temporary+string(os.PathSeparator)) || !strings.Contains(path, "tty-graphics-protocol") {
			return nil, errors.New("EINVAL: temporary file not in temp dir")
		}
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errImageFile
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errImageFile
	}
	offset, size, err := mediaRange(opened.Size(), keys)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.NewSectionReader(file, offset, size))
	if err != nil || int64(len(data)) != size {
		return nil, errImageFile
	}
	if medium == "t" {
		current, err := os.Stat(path)
		if err != nil || !os.SameFile(opened, current) || os.Remove(path) != nil {
			return nil, errImageFile
		}
	}
	return data, nil
}

func mediaRange(total int64, keys map[string]string) (int64, int64, error) {
	offset := int64(number(keys, "O", 0))
	size := int64(number(keys, "S", int(total-offset)))
	if offset < 0 || size < 0 || offset > total || size > total-offset {
		return 0, 0, errImageFile
	}
	return offset, size, nil
}

func sharedMemoryPayload(keys map[string]string, name string) ([]byte, error) {
	if !strings.HasPrefix(name, "/") || len(name) < 2 || strings.ContainsAny(name[1:], "/\x00") {
		return nil, errImageFile
	}
	fd, err := shmOpen(name, unix.O_RDONLY, 0)
	if err != nil {
		return nil, errImageFile
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Size <= 0 || int64(int(stat.Size)) != stat.Size {
		return nil, errImageFile
	}
	// Darwin rounds SHM storage to whole pages. Raw pixels still contain
	// exactly the declared image dimensions, not those trailing page bytes.
	format := number(keys, "f", 32)
	if _, explicit := keys["S"]; !explicit && keys["o"] == "" && (format == 24 || format == 32) {
		width, height, channels := int64(number(keys, "s", 0)), int64(number(keys, "v", 0)), int64(format/8)
		if width > 0 && height > 0 {
			if height > stat.Size/channels || width > stat.Size/(height*channels) {
				return nil, errImageFile
			}
			keys["S"] = strconv.FormatInt(width*height*channels, 10)
		}
	}
	offset, size, err := mediaRange(stat.Size, keys)
	if err != nil {
		return nil, err
	}
	memory, err := unix.Mmap(fd, 0, int(stat.Size), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, errImageFile
	}
	data := append([]byte(nil), memory[offset:offset+size]...)
	if unix.Munmap(memory) != nil || shmUnlink(name) != nil {
		return nil, errImageFile
	}
	return data, nil
}
