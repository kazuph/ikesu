package kitty

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNativeSharedMemoryReadAndUnlink(t *testing.T) {
	name := createTestSharedMemory(t, []byte{255, 0, 0, 0, 255, 0})
	reader, err := shmOpen(name, unix.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	unix.Close(reader)
	if err := shmUnlink(name); err != nil {
		t.Fatal(err)
	}
	if remaining, err := shmOpen(name, unix.O_RDONLY, 0); err == nil {
		unix.Close(remaining)
		t.Fatal("unlinked shared memory is still named")
	}
}

func createTestSharedMemory(t *testing.T, data []byte) string {
	t.Helper()
	name := fmt.Sprintf("/tb-%x", rand.Text()[:12])
	fd, err := shmOpen(name, unix.O_CREAT|unix.O_EXCL|unix.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	t.Cleanup(func() { shmUnlink(name) })
	if err := unix.Ftruncate(fd, int64(len(data))); err != nil {
		t.Fatal(err)
	}
	memory, err := unix.Mmap(fd, 0, len(data), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		t.Fatal(err)
	}
	copy(memory, data)
	if err := unix.Munmap(memory); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestSharedMemoryMediaQueryDisplayAndRange(t *testing.T) {
	pixels := []byte{255, 0, 0, 0, 255, 0}
	for _, action := range []string{"q", "T"} {
		t.Run(action, func(t *testing.T) {
			name := createTestSharedMemory(t, append(append([]byte{1, 2}, pixels...), 3))
			var images Store
			command := fmt.Sprintf("Ga=%s,t=s,f=24,s=2,v=1,i=10,c=4,r=2,C=1,O=2,S=6;%s", action, base64.StdEncoding.EncodeToString([]byte(name)))
			reply, _, _, _ := images.Handle([]byte(command), 0, 0, 8, 16, false)
			if !strings.Contains(reply, ";OK") {
				t.Fatalf("shared memory medium failed: %q", reply)
			}
			if fd, err := shmOpen(name, unix.O_RDONLY, 0); err == nil {
				unix.Close(fd)
				t.Fatal("consumed shared memory was not unlinked")
			}
			if action == "q" {
				if len(images.Images) != 0 || len(images.Placements) != 0 {
					t.Fatal("shared-memory query displayed an image")
				}
			} else if len(images.Placements) != 1 || !bytes.Equal(images.Placements[0].Image.Data, pixels) {
				t.Fatal("shared memory did not display the requested source range")
			}
		})
	}
}

func TestSharedMemoryMediaUsesDeclaredPixelsWithoutExplicitSize(t *testing.T) {
	pixels := []byte{255, 0, 0, 0, 255, 0}
	for _, action := range []string{"q", "T"} {
		name := createTestSharedMemory(t, pixels)
		var images Store
		command := fmt.Sprintf("Ga=%s,t=s,f=24,s=2,v=1,i=10,c=4,r=2,C=1;%s", action, base64.StdEncoding.EncodeToString([]byte(name)))
		reply, _, _, _ := images.Handle([]byte(command), 0, 0, 8, 16, false)
		if !strings.Contains(reply, ";OK") {
			t.Fatalf("page-aligned SHM was mistaken for extra pixels: %q", reply)
		}
		if action == "T" && !bytes.Equal(images.Images[10].Data, pixels) {
			t.Fatal("trailing SHM page bytes entered the image")
		}
	}
}
