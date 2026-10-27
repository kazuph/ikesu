package client

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/kitty"
	"golang.org/x/sys/unix"
)

func graphicsFileTransport(in, out *os.File, remote bool) string {
	if kitty.SupportsTerminal(in, out, remote) {
		return endpoint.DirectKittyTransport
	}
	return ""
}

func validateGraphicsFile(message endpoint.GraphicsFile) error {
	if !filepath.IsAbs(message.Path) || filepath.Clean(message.Path) != message.Path || message.ExpectedLen == 0 || message.ExpectedLen > api.MaxGraphicsFileDirectBytes {
		return errors.New("invalid local graphics file path or length")
	}
	source := filepath.Dir(message.Path)
	server := filepath.Dir(source)
	base := filepath.Dir(server)
	if filepath.Base(source) != "source" || !strings.HasPrefix(filepath.Base(server), "server-") || filepath.Base(base) != fmt.Sprintf("herdr-pane-graphics-%d", os.Geteuid()) {
		return errors.New("graphics path is outside an advertised private source")
	}
	for _, directory := range []string{base, server, source} {
		info, err := os.Lstat(directory)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || int(stat.Uid) != os.Geteuid() {
			return errors.New("graphics source directory is not private")
		}
	}
	fd, err := unix.Open(message.Path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0777 != 0600 || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 || stat.Size < 0 || uint64(stat.Size) != message.ExpectedLen {
		return errors.New("invalid local graphics source identity or size")
	}
	var named unix.Stat_t
	if err := unix.Lstat(message.Path, &named); err != nil || named.Dev != stat.Dev || named.Ino != stat.Ino || named.Mode != stat.Mode || named.Size != stat.Size {
		return errors.New("local graphics source path changed")
	}
	if message.Leading != "" {
		var row, column uint32
		if _, err := fmt.Sscanf(message.Leading, "\x1b[%d;%dH", &row, &column); err != nil || row == 0 || column == 0 || message.Leading != fmt.Sprintf("\x1b[%d;%dH", row, column) {
			return errors.New("invalid graphics cursor position")
		}
	}
	fields := map[string]string{}
	for _, field := range strings.Split(message.Control, ",") {
		key, value, ok := strings.Cut(field, "=")
		if !ok || fields[key] != "" || value == "" {
			return errors.New("invalid graphics control fields")
		}
		fields[key] = value
		if key == "a" {
			if value != "t" && value != "T" {
				return errors.New("invalid graphics transmission action")
			}
			continue
		}
		switch key {
		case "f", "s", "v", "i", "q", "p", "c", "r", "z", "C", "x", "y", "w", "h", "X", "Y":
		default:
			return errors.New("unsupported graphics transmission control")
		}
		number := value
		if key == "z" {
			number = strings.TrimPrefix(number, "-")
		}
		for _, digit := range number {
			if digit < '0' || digit > '9' {
				return errors.New("non-numeric graphics control")
			}
		}
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return err
		}
	}
	width, errW := strconv.ParseUint(fields["s"], 10, 32)
	height, errH := strconv.ParseUint(fields["v"], 10, 32)
	if fields["C"] != "" && fields["C"] != "1" || fields["a"] == "T" && fields["C"] != "1" {
		return errors.New("graphics transmission must preserve the cursor")
	}
	if fields["a"] == "" || fields["f"] != "32" || fields["q"] != "0" || fields["i"] != strconv.FormatUint(uint64(message.ImageID), 10) || errW != nil || errH != nil || width == 0 || height == 0 || width > api.MaxGraphicsFileDirectBytes/4/height || width*height*4 != message.ExpectedLen {
		return errors.New("graphics control does not match the declared source")
	}
	return nil
}

func writeGraphicsFile(out *os.File, message endpoint.GraphicsFile) error {
	if err := validateGraphicsFile(message); err != nil {
		return err
	}
	data := "\x1b7" + message.Leading + "\x1b_G" + message.Control + ",t=f;" + base64.StdEncoding.EncodeToString([]byte(message.Path)) + "\x1b\\\x1b8"
	n, err := io.WriteString(out, data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}
