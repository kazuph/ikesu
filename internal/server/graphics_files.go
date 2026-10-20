package server

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/kazuph/ikesu/internal/api"
	"golang.org/x/sys/unix"
)

type graphicsFileSource struct {
	base, server, source string
}

func privateGraphicsDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || int(stat.Uid) != os.Geteuid() {
		return nil, errors.New("graphics directory must be private and owned by this user")
	}
	return info, nil
}

func newGraphicsFileSource() (*graphicsFileSource, error) {
	root := os.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(root) {
		root = "/var/tmp"
	}
	base := filepath.Join(root, fmt.Sprintf("herdr-pane-graphics-%d", os.Geteuid()))
	if err := os.Mkdir(base, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if _, err := privateGraphicsDirectory(base); err != nil {
		return nil, err
	}
	retireGraphicsFileSources(base)
	// Mark our nonce so lifecycle cleanup never claims another application's
	// directories under the shared advertised base.
	server, err := os.MkdirTemp(base, fmt.Sprintf("server-%d-gtx-", os.Getpid()))
	if err != nil {
		return nil, err
	}
	source := filepath.Join(server, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		_ = os.RemoveAll(server)
		return nil, err
	}
	return &graphicsFileSource{base: base, server: server, source: source}, nil
}

func (s *graphicsFileSource) close() { _ = os.RemoveAll(s.server) }

func retireGraphicsFileSources(base string) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, entry := range entries {
		pidText, nonce, ok := strings.Cut(strings.TrimPrefix(entry.Name(), "server-"), "-gtx-")
		pid, err := strconv.Atoi(pidText)
		if !strings.HasPrefix(entry.Name(), "server-") || !ok || nonce == "" || err != nil || pid <= 0 || unix.Kill(pid, 0) != unix.ESRCH {
			continue
		}
		path := filepath.Join(base, entry.Name())
		if _, err := privateGraphicsDirectory(path); err == nil {
			_ = os.RemoveAll(path)
		}
	}
}

func graphicsFileFrameBytes(frame api.PaneGraphicsStreamFrame, direct bool) (uint64, *api.Error) {
	if frame.Format != "rgba" && frame.Format != "bgra" {
		return 0, &api.Error{Code: "invalid_frame", Message: "file frames require rgba or bgra"}
	}
	limit := uint64(api.MaxGraphicsFileInlineBytes)
	if direct && frame.Format == "rgba" && (frame.LayerID == nil || *frame.LayerID == "primary") {
		limit = api.MaxGraphicsFileDirectBytes
	}
	width, height := uint64(frame.ImageWidth), uint64(frame.ImageHeight)
	if width == 0 || height == 0 || width > limit/4/height {
		return 0, &api.Error{Code: "invalid_image", Message: "file frame dimensions exceed the available transport limit"}
	}
	return width * height * 4, nil
}

type graphicsSourceFile struct {
	file      *os.File
	source    *graphicsFileSource
	directory os.FileInfo
	identity  os.FileInfo
	expected  int64
}

func (s *graphicsFileSource) open(path string, expected uint64) (*graphicsSourceFile, error) {
	if expected == 0 || expected > api.MaxGraphicsFileDirectBytes || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) != s.source {
		return nil, errors.New("invalid graphics source path or length")
	}
	directory, err := privateGraphicsDirectory(s.source)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := &graphicsSourceFile{file: os.NewFile(uintptr(fd), path), source: s, directory: directory, expected: int64(expected)}
	if f.identity, err = f.file.Stat(); err == nil {
		err = f.check()
	}
	if err != nil {
		f.file.Close()
		return nil, err
	}
	return f, nil
}

func (f *graphicsSourceFile) check() error {
	for _, path := range []string{f.source.base, f.source.server, f.source.source} {
		directory, err := privateGraphicsDirectory(path)
		if err != nil || path == f.source.source && !os.SameFile(f.directory, directory) {
			return errors.New("graphics source directory changed")
		}
	}
	current, err := os.Lstat(f.file.Name())
	if err != nil || !os.SameFile(f.identity, current) {
		return errors.New("graphics source file changed")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(f.file.Fd()), &stat); err != nil {
		return err
	}
	if !current.Mode().IsRegular() || current.Mode().Perm() != 0600 || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 || stat.Size != f.expected {
		return errors.New("graphics source must be a private regular file with exactly the declared pixels")
	}
	return nil
}

func (f *graphicsSourceFile) ownedPixels() ([]byte, error) {
	if f.expected > api.MaxGraphicsFileInlineBytes {
		return nil, errors.New("graphics source exceeds inline limit")
	}
	if err := f.check(); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.NewSectionReader(f.file, 0, f.expected))
	if err != nil || int64(len(data)) != f.expected {
		return nil, errors.New("graphics source read was incomplete")
	}
	if err := f.check(); err != nil {
		return nil, err
	}
	return data, nil
}
