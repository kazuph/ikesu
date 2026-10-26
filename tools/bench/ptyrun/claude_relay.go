package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// An ordinary terminal around the unmodified Claude Code executable. It never
// forwards conversation input. Owned controller files request ordinary Ctrl-C
// exit or the parent's single explicitly authorized temporary-folder trust key.
func runClaudeRelay(args []string, logPath, closePath string) {
	size, err := pty.GetsizeFull(os.Stdin)
	if err != nil {
		panic(err)
	}
	command := exec.Command(args[0], args[1:]...)
	command.Env = os.Environ()
	terminal, err := pty.StartWithSize(command, size)
	if err != nil {
		panic(err)
	}
	defer terminal.Close()
	emu := vt.NewEmulator(int(size.Cols), int(size.Rows))
	observer := newTimeline(logPath, emu, int(size.Cols), int(size.Rows))
	defer observer.close()
	observer.reply = terminal
	observer.receiver.reply = terminal
	var mu sync.Mutex
	var sizeSequence int
	var resizedAt int64
	resizeHash := textHash(terminalLines(emu, int(size.Cols), int(size.Rows)))
	changedSinceResize := false
	observer.onFrame = func(at int64) {
		if at < resizedAt {
			return
		}
		lines := terminalLines(emu, int(size.Cols), int(size.Rows))
		hash := textHash(lines)
		changedSinceResize = changedSinceResize || hash != resizeHash
		observer.record(map[string]any{"type": "claude.frame", "clock_ns": at, "size_sequence": sizeSequence, "width": size.Cols, "height": size.Rows, "lines": lines, "text_hash": hash, "synchronized": observer.synchronizedFrame, "stdout_bytes": observer.total, "changed_since_resize": changedSinceResize})
	}
	observer.record(map[string]any{"type": "claude.started", "clock_ns": monotonicNS(), "pid": command.Process.Pid, "relay_pid": os.Getpid(), "cwd": func() string { p, _ := os.Getwd(); return p }(), "args": args})
	stop := make(chan struct{})
	defer close(stop)
	changes := make(chan os.Signal, 1)
	signal.Notify(changes, syscall.SIGWINCH)
	defer signal.Stop(changes)
	go func() {
		for {
			select {
			case <-changes:
				next, err := pty.GetsizeFull(os.Stdin)
				if err != nil {
					continue
				}
				mu.Lock()
				if next.Cols != size.Cols || next.Rows != size.Rows {
					if err := pty.Setsize(terminal, next); err != nil {
						mu.Unlock()
						panic(err)
					}
					resizedAt = monotonicNS()
					sizeSequence++
					size = next
					emu.Resize(int(size.Cols), int(size.Rows))
					resizeHash = textHash(terminalLines(emu, int(size.Cols), int(size.Rows)))
					changedSinceResize = false
					observer.cols, observer.rows = int(size.Cols), int(size.Rows)
					observer.record(map[string]any{"type": "claude.pty_size", "clock_ns": resizedAt, "size_sequence": sizeSequence, "width": size.Cols, "height": size.Rows})
				}
				mu.Unlock()
			case <-stop:
				return
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		trustSent := false
		for {
			select {
			case <-ticker.C:
				if !trustSent {
					if _, err := os.Stat(closePath + ".trust"); err == nil {
						observer.record(map[string]any{"type": "claude.trust_selected", "clock_ns": monotonicNS()})
						_, _ = terminal.Write([]byte{'\r'})
						trustSent = true
					}
				}
				if _, err := os.Stat(closePath); err == nil {
					observer.record(map[string]any{"type": "claude.exit_requested", "clock_ns": monotonicNS()})
					_, _ = terminal.Write([]byte{3})
					time.Sleep(300 * time.Millisecond)
					_, _ = terminal.Write([]byte{3})
					return
				}
			case <-stop:
				return
			}
		}
	}()
	// Protocol replies are ordinary terminal responses, not conversation input.
	go func() { _, _ = io.Copy(terminal, emu) }()
	buffer := make([]byte, 1<<16)
	for {
		n, readErr := terminal.Read(buffer)
		at := monotonicNS()
		if n > 0 {
			if _, err := os.Stdout.Write(buffer[:n]); err != nil {
				break
			}
			mu.Lock()
			observer.feed(buffer[:n], at)
			mu.Unlock()
		}
		if readErr != nil {
			break
		}
	}
	err = command.Wait()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = -1
	}
	observer.record(map[string]any{"type": "claude.exited", "clock_ns": monotonicNS(), "exit": code})
	data, _ := json.Marshal(map[string]any{"pid": command.Process.Pid, "relay_pid": os.Getpid(), "exit": code})
	_ = os.WriteFile(closePath+".exit", data, 0600)
}
