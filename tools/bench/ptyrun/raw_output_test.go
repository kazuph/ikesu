package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSlowRawLogDoesNotBackpressureMeasuredPTY(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "ptyrun")
	if output, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build observer: %v\n%s", err, output)
	}
	rawPath, donePath := filepath.Join(dir, "raw.fifo"), filepath.Join(dir, "producer-done")
	if err := unix.Mkfifo(rawPath, 0600); err != nil {
		t.Fatal(err)
	}
	steps := filepath.Join(dir, "steps")
	if err := os.WriteFile(steps, []byte("waitfile "+donePath+" 5s\nscreen "+filepath.Join(dir, "screen")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Pause a real FIFO sink until the real PTY producer has finished.
	drain := make(chan struct{})
	defer close(drain)
	logged := make(chan []byte, 1)
	logError := make(chan error, 1)
	go func() {
		f, err := os.Open(rawPath)
		if err != nil {
			logError <- err
			return
		}
		defer f.Close()
		<-drain
		data, err := io.ReadAll(f)
		if err != nil {
			logError <- err
			return
		}
		logged <- data
	}()
	chunk := strings.Repeat("0123456789abcdef", 64)
	program := `stty raw -echo; i=0; while [ "$i" -lt 1024 ]; do printf '%s' "$1"; i=$((i+1)); done; printf END; printf done > "$2"; exec /bin/cat`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-cols", "80", "-rows", "24", "-raw-output", rawPath, "-steps", steps, "--", "/bin/sh", "-c", program, "producer", chunk, donePath)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		if _, err := os.Stat(donePath); err == nil {
			break
		}
		select {
		case <-tick.C:
		case err := <-exited:
			t.Fatalf("observer exited before producer completed: %v\n%s", err, output.Bytes())
		case <-deadline.C:
			t.Fatal("slow artifact storage blocked the measured PTY producer")
		}
	}
	drain <- struct{}{}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("observer: %v\n%s", err, output.Bytes())
		}
	case <-ctx.Done():
		t.Fatal("observer did not drain its output queue")
	}
	select {
	case data := <-logged:
		want := strings.Repeat(chunk, 1024) + "END"
		if string(data) != want {
			t.Fatalf("raw log order/content changed: got %d bytes want %d", len(data), len(want))
		}
	case err := <-logError:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("raw log did not close after observer exit")
	}
}
