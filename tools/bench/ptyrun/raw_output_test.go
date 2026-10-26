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

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func TestInputWritePrecedesBlockedTimelineStorage(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	emu := vt.NewEmulator(80, 24)
	defer emu.Close()
	observer := newTimeline(filepath.Join(t.TempDir(), "timeline"), emu, 80, 24)
	defer observer.close()
	observer.mu.Lock()
	locked := true
	defer func() {
		if locked {
			observer.mu.Unlock()
		}
	}()
	event := map[string]any{"type": "input"}
	finished := make(chan struct{})
	go func() { recordMouseInput(observer, slave, event, 32, 27, 10, false); close(finished) }()
	fds := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN}}
	if ready, err := unix.Poll(fds, 5000); err != nil || ready == 0 {
		t.Fatal("timeline storage delayed the physical input write", ready, err)
	}
	data := make([]byte, len("\x1b[<32;27;10M"))
	if _, err := io.ReadFull(master, data); err != nil || string(data) != "\x1b[<32;27;10M" {
		t.Fatal("physical mouse bytes changed", data, err)
	}
	select {
	case <-finished:
		t.Fatal("timeline storage lock was bypassed")
	default:
	}
	observer.mu.Unlock()
	locked = false
	<-finished
	if event["clock_ns"].(int64) > monotonicNS() {
		t.Fatal("input clock lies after physical delivery")
	}
}

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
