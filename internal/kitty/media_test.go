package kitty

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFileMediaQueryAndDisplayLifetimes(t *testing.T) {
	pixels := []byte{255, 0, 0, 0, 255, 0}
	for _, medium := range []string{"f", "t"} {
		for _, action := range []string{"q", "T"} {
			t.Run(medium+action, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "tty-graphics-protocol-image")
				if err := os.WriteFile(path, pixels, 0600); err != nil {
					t.Fatal(err)
				}
				var images Store
				command := fmt.Sprintf("Ga=%s,t=%s,f=24,s=2,v=1,i=10,c=4,r=2,C=1;%s", action, medium, base64.StdEncoding.EncodeToString([]byte(path)))
				reply, _, _, _ := images.Handle([]byte(command), 0, 0, 8, 16, false)
				if !strings.Contains(reply, ";OK") {
					t.Fatalf("file medium failed: %q", reply)
				}
				_, err := os.Stat(path)
				if medium == "f" && err != nil || medium == "t" && !os.IsNotExist(err) {
					t.Fatalf("wrong file lifetime after %s: %v", medium, err)
				}
				if action == "q" {
					if len(images.Images) != 0 || len(images.Placements) != 0 {
						t.Fatal("query stored or displayed a probe image")
					}
				} else if len(images.Placements) != 1 || !bytes.Equal(images.Placements[0].Image.Data, pixels) {
					t.Fatal("file medium did not display its actual pixels")
				}
			})
		}
	}
}

func TestFileMediaRangeSymlinkAndSpecialFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "image")
	pixels := []byte{255, 0, 0, 0, 255, 0}
	if err := os.WriteFile(path, append(append([]byte{1, 2}, pixels...), 3), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	var images Store
	command := "Ga=T,t=f,f=24,s=2,v=1,i=10,c=4,r=2,C=1,O=2,S=6;" + base64.StdEncoding.EncodeToString([]byte(link))
	reply, _, _, _ := images.Handle([]byte(command), 0, 0, 8, 16, false)
	if !strings.Contains(reply, ";OK") || !bytes.Equal(images.Images[10].Data, pixels) {
		t.Fatalf("file range did not follow the symlink to real pixels: %q", reply)
	}
	fifo := filepath.Join(root, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{fifo, root, filepath.Join(root, "missing")} {
		command = "Ga=q,t=f,f=24,s=2,v=1,i=10;" + base64.StdEncoding.EncodeToString([]byte(invalid))
		reply, _, _, _ = images.Handle([]byte(command), 0, 0, 8, 16, false)
		if !strings.Contains(reply, ";EBADF:Failed to read image file") {
			t.Fatalf("file failure exposed a path distinction: %q", reply)
		}
	}
}

func TestTemporaryMediaRejectsUnmarkedFileWithoutDeleting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unrelated-file")
	if err := os.WriteFile(path, []byte{0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	var images Store
	command := "Ga=q,t=t,f=24,s=1,v=1,i=10;" + base64.StdEncoding.EncodeToString([]byte(path))
	reply, _, _, _ := images.Handle([]byte(command), 0, 0, 8, 16, false)
	if strings.Contains(reply, ";OK") {
		t.Fatal("unmarked temporary file was accepted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("rejected temporary file was deleted")
	}
}
