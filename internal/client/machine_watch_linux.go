package client

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

func watchMachineCatalog(parent context.Context, path string, changed func(context.Context, error)) (func(), error) {
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return nil, err
	}
	_, err = unix.InotifyAddWatch(fd, filepath.Dir(path), unix.IN_CLOSE_WRITE|unix.IN_MOVED_TO|unix.IN_DELETE)
	if err != nil {
		unix.Close(fd)
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "machine-catalog-watch")
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); file.Close(); <-done }) }
	go func() {
		defer close(done)
		defer file.Close()
		buf := make([]byte, unix.SizeofInotifyEvent+unix.NAME_MAX+1)
		for {
			n, err := file.Read(buf)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				changed(ctx, err)
				return
			}
			for offset := 0; offset+unix.SizeofInotifyEvent <= n; {
				length := int(binary.NativeEndian.Uint32(buf[offset+12 : offset+16]))
				end := offset + unix.SizeofInotifyEvent + length
				if end > n {
					changed(ctx, unix.EINVAL)
					return
				}
				name := strings.TrimRight(string(buf[offset+unix.SizeofInotifyEvent:end]), "\x00")
				if name == filepath.Base(path) {
					changed(ctx, nil)
				}
				offset = end
			}
		}
	}()
	return stop, nil
}
