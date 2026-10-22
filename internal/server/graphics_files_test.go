package server

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"golang.org/x/sys/unix"
)

func graphicsSourceFixture(t *testing.T) (*graphicsFileSource, string, []byte) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	source, err := newGraphicsFileSource()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.close)
	path := filepath.Join(source.source, "frame.rgba")
	data := []byte{0, 1, 2, 255, 3, 4, 5, 255, 6, 7, 8, 255, 9, 10, 11, 255, 12, 13, 14, 255, 15, 16, 17, 255}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return source, path, data
}

func TestGraphicsSourceFileRetainsProducerAndOwnsInlinePixels(t *testing.T) {
	source, path, pixels := graphicsSourceFixture(t)
	for _, dir := range []string{source.base, source.server, source.source} {
		if _, err := privateGraphicsDirectory(dir); err != nil {
			t.Fatal(err)
		}
	}
	file, err := source.open(path, uint64(len(pixels)))
	if err != nil {
		t.Fatal(err)
	}
	defer file.file.Close()
	owned, err := file.ownedPixels()
	if err != nil || !bytes.Equal(owned, pixels) {
		t.Fatalf("inline pixels=%x error=%v", owned, err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{255}, len(pixels)), 0600); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(owned, pixels) {
		t.Fatal("producer reuse changed the owned inline pixels")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("reading the source removed the producer file", err)
	}
	file.file.Close()
	source.close()
	if _, err := os.Stat(source.server); !os.IsNotExist(err) {
		t.Fatalf("server source tree survived close: %v", err)
	}
	if _, err := os.Stat(source.base); err != nil {
		t.Fatal("cleanup removed the shared base", err)
	}
}

func TestGraphicsSourceFileRejectsInvalidPathsPermissionsAndLengths(t *testing.T) {
	for _, name := range []string{"relative", "outside", "descendant", "symlink", "hardlink", "mode", "directory", "fifo", "short", "long", "zero", "overflow", "source-symlink"} {
		t.Run(name, func(t *testing.T) {
			source, path, pixels := graphicsSourceFixture(t)
			expected := uint64(len(pixels))
			var err error
			switch name {
			case "relative":
				path = "frame.rgba"
			case "outside":
				path = filepath.Join(t.TempDir(), "frame.rgba")
				err = os.WriteFile(path, pixels, 0600)
			case "descendant":
				err = os.Mkdir(filepath.Join(source.source, "child"), 0700)
				path = filepath.Join(source.source, "child", "frame.rgba")
				if err == nil {
					err = os.WriteFile(path, pixels, 0600)
				}
			case "symlink":
				link := filepath.Join(source.source, "link.rgba")
				err = os.Symlink(path, link)
				path = link
			case "hardlink":
				err = os.Link(path, filepath.Join(source.source, "other.rgba"))
			case "mode":
				err = os.Chmod(path, 0644)
			case "directory":
				path = filepath.Join(source.source, "directory")
				err = os.Mkdir(path, 0700)
			case "fifo":
				path = filepath.Join(source.source, "fifo")
				err = unix.Mkfifo(path, 0600)
			case "short":
				err = os.Truncate(path, int64(len(pixels)-1))
			case "long":
				err = os.Truncate(path, int64(len(pixels)+1))
			case "zero":
				expected = 0
			case "overflow":
				expected = ^uint64(0)
			case "source-symlink":
				moved := source.source + "-moved"
				err = os.Rename(source.source, moved)
				if err == nil {
					err = os.Symlink(moved, source.source)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if file, err := source.open(path, expected); err == nil {
				file.file.Close()
				t.Fatal("accepted invalid producer file", name)
			}
		})
	}
}

func TestGraphicsSourceFileRejectsReplacementBeforeReceipt(t *testing.T) {
	source, path, pixels := graphicsSourceFixture(t)
	file, err := source.open(path, uint64(len(pixels)))
	if err != nil {
		t.Fatal(err)
	}
	defer file.file.Close()
	if err := os.Rename(path, path+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pixels, 0600); err != nil {
		t.Fatal(err)
	}
	if err := file.check(); err == nil {
		t.Fatal("accepted a replaced source inode before receipt")
	}
}

func TestGraphicsSourceRetiresOnlyDeadOwnedServerTrees(t *testing.T) {
	source, _, _ := graphicsSourceFixture(t)
	child := exec.Command("/usr/bin/true")
	if err := child.Run(); err != nil {
		t.Fatal(err)
	}
	dead := filepath.Join(source.base, fmt.Sprintf("server-%d-ikesu-stale", child.Process.Pid))
	other := filepath.Join(source.base, fmt.Sprintf("server-%d-reference", child.Process.Pid))
	for _, path := range []string{dead, other} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "frame.rgba"), []byte("owned pixels"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	retireGraphicsFileSources(source.base)
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatal("dead ikesu source tree was not retired", err)
	}
	for _, path := range []string{other, source.server} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("retired another application's or a live server's source tree", path, err)
		}
	}
}

func TestGraphicsFileFrameTransportBounds(t *testing.T) {
	secondary := "secondary"
	for _, tt := range []struct {
		name          string
		format        string
		width, height uint32
		layer         *string
		direct        bool
		want          uint64
		code          string
	}{
		{"inline-bound", "rgba", api.MaxGraphicsFileInlineBytes / 4, 1, nil, false, api.MaxGraphicsFileInlineBytes, ""},
		{"inline-over", "rgba", api.MaxGraphicsFileInlineBytes/4 + 1, 1, nil, false, 0, "invalid_image"},
		{"direct-bound", "rgba", api.MaxGraphicsFileDirectBytes / 4, 1, nil, true, api.MaxGraphicsFileDirectBytes, ""},
		{"direct-over", "rgba", api.MaxGraphicsFileDirectBytes/4 + 1, 1, nil, true, 0, "invalid_image"},
		{"bgra-inline", "bgra", api.MaxGraphicsFileInlineBytes/4 + 1, 1, nil, true, 0, "invalid_image"},
		{"secondary-inline", "rgba", api.MaxGraphicsFileInlineBytes/4 + 1, 1, &secondary, true, 0, "invalid_image"},
		{"overflow", "rgba", ^uint32(0), ^uint32(0), nil, true, 0, "invalid_image"},
		{"zero-height", "rgba", 2, 0, nil, true, 0, "invalid_image"},
		{"zero-width", "rgba", 0, 3, nil, true, 0, "invalid_image"},
		{"png", "png", 2, 3, nil, true, 0, "invalid_frame"},
		{"rgb", "rgb", 2, 3, nil, true, 0, "invalid_frame"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			frame := api.PaneGraphicsStreamFrame{PaneGraphicsSetParams: api.PaneGraphicsSetParams{
				Format: tt.format, ImageWidth: tt.width, ImageHeight: tt.height, LayerID: tt.layer}}
			size, failure := graphicsFileFrameBytes(frame, tt.direct)
			if size != tt.want || tt.code == "" && failure != nil || tt.code != "" && (failure == nil || failure.Code != tt.code) {
				t.Fatalf("size=%d failure=%+v, want %d/%s", size, failure, tt.want, tt.code)
			}
		})
	}
}
