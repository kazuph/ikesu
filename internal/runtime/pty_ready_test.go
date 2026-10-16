package runtime

import (
	"os"
	"testing"
)

func TestPTYReadyPreservesBytesAndEOF(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	fd := int(r.Fd())
	if ptyReadyNow(fd) {
		t.Fatal("idle pipe is readable")
	}
	if _, err := w.WriteString("preserved"); err != nil {
		t.Fatal(err)
	}
	if !ptyReadyNow(fd) || !ptyReadyNow(fd) {
		t.Fatal("probe consumed or cleared pending output")
	}
	buf := make([]byte, len("preserved"))
	if n, err := r.Read(buf); err != nil || string(buf[:n]) != "preserved" {
		t.Fatalf("output = %q, err = %v", buf[:n], err)
	}
	if ptyReadyNow(fd) {
		t.Fatal("drained pipe is readable")
	}
	w.Close()
	if !ptyReadyNow(fd) {
		t.Fatal("closed producer must allow reader to observe EOF")
	}
}
