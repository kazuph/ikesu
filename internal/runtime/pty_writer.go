package runtime

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// ptyInputLimit bounds input queued for an application that stopped reading.
// Writes beyond it are refused whole instead of growing without limit.
const ptyInputLimit = 8 << 20

// ptyWriteChunk bounds one non-blocking write; the writer checks for a
// handoff pause between chunks.
const ptyWriteChunk = 64 << 10

// handoffWriterWait bounds how long a handoff waits for a pane's writer to
// stop between chunks before refusing the handoff.
const handoffWriterWait = 2 * time.Second

// ErrPTYInputFull reports that the application has not read its queued input.
var ErrPTYInputFull = errors.New("pane input not accepted: the application is not reading its input")

var errPaneClosed = errors.New("pane closed: input not accepted")

// ptyWriter is the only writer of a pane's PTY. Every input path (keys,
// pastes, mouse and terminal responses from the emulator, mail, screen
// answers) appends whole writes to one ordered queue and returns at once, so
// an application that stops reading cannot stall the app loop or a lock
// holder (2026-11-09: a full PTY froze the server for eight hours). Accepted
// bytes are carried across a live handoff.
type ptyWriter struct {
	mu      sync.Mutex
	queue   []byte
	stopped error
	paused  bool
	ready   chan struct{}
	idle    chan struct{} // closed when the running writer returns
	dropped int
}

func (w *ptyWriter) signal() {
	select {
	case w.ready <- struct{}{}:
	default:
	}
}

// enqueue accepts all of data or none of it, without waiting.
func (w *ptyWriter) enqueue(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	w.mu.Lock()
	switch {
	case w.stopped != nil:
		err := w.stopped
		w.mu.Unlock()
		return err
	case len(w.queue)+len(data) > ptyInputLimit:
		w.dropped += len(data)
		w.mu.Unlock()
		return ErrPTYInputFull
	}
	w.queue = append(w.queue, data...)
	w.mu.Unlock()
	w.signal()
	return nil
}

func (w *ptyWriter) stop(err error) {
	w.mu.Lock()
	if w.stopped == nil {
		w.stopped = err
	}
	w.queue = nil
	w.mu.Unlock()
}

// responseWriter hands emulator output (responses and encoded input) to the
// pane writer in call order. It never blocks; a refused write is counted.
type responseWriter struct{ p *Pane }

func (r responseWriter) Write(b []byte) (int, error) {
	_ = r.p.writer.enqueue(b)
	return len(b), nil
}

// startPTYWriter runs the writer until the PTY fails, the pane ends, or a
// handoff pauses it.
func (p *Pane) startPTYWriter() {
	w := &p.writer
	w.mu.Lock()
	if w.ready == nil {
		w.ready = make(chan struct{}, 1)
	}
	w.paused = false
	idle := make(chan struct{})
	w.idle = idle
	w.mu.Unlock()
	go func() {
		defer close(idle)
		for {
			select {
			case <-w.ready:
			case <-p.done:
				w.stop(errPaneClosed)
				return
			}
			for {
				if p.pausing.Load() {
					w.mu.Lock()
					w.paused = true
					w.mu.Unlock()
					return
				}
				w.mu.Lock()
				n := min(len(w.queue), ptyWriteChunk)
				chunk := append([]byte(nil), w.queue[:n]...)
				w.mu.Unlock()
				if n == 0 {
					break
				}
				if !p.waitWritable() {
					if p.pausing.Load() {
						continue
					}
					w.stop(errPaneClosed)
					return
				}
				written, err := writeSomePTY(p.currentPTY(), chunk)
				w.mu.Lock()
				if written <= len(w.queue) {
					w.queue = w.queue[written:]
				}
				w.mu.Unlock()
				if err != nil {
					w.stop(err)
					p.reportDroppedInput()
					return
				}
			}
		}
	}()
}

// waitWritable waits until the PTY accepts input. It returns false when a
// handoff pause starts or the PTY is gone.
func (p *Pane) waitWritable() bool {
	if p.wakeRead == nil {
		return true
	}
	for {
		fds := []unix.PollFd{{Fd: int32(p.ptyFD), Events: unix.POLLOUT}, {Fd: int32(p.wakeFD), Events: unix.POLLIN}}
		_, err := unix.Poll(fds, 1000)
		if err == unix.EINTR {
			continue
		}
		if err != nil || p.pausing.Load() {
			return false
		}
		if fds[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			return false
		}
		if fds[0].Revents&unix.POLLOUT != 0 {
			return true
		}
		select {
		case <-p.done:
			return false
		default:
		}
	}
}

// pauseWriterForHandoff stops the writer between chunks and returns the
// accepted bytes it has not written, for the importing server to send first.
// The caller has set p.pausing and woken the PTY wait.
func (p *Pane) pauseWriterForHandoff() ([]byte, error) {
	w := &p.writer
	w.signal()
	w.mu.Lock()
	idle := w.idle
	w.mu.Unlock()
	if idle != nil {
		select {
		case <-idle:
		case <-time.After(handoffWriterWait):
			return nil, fmt.Errorf("pane %d: the application has not read its input; handoff refused so no input is lost", p.ID)
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.queue...), nil
}

// resumeWriterAfterHandoff restarts a writer that a refused handoff paused.
func (p *Pane) resumeWriterAfterHandoff() {
	w := &p.writer
	w.mu.Lock()
	paused := w.paused
	w.mu.Unlock()
	if paused {
		p.startPTYWriter()
		w.signal()
	}
}

func (p *Pane) reportDroppedInput() {
	p.writer.mu.Lock()
	dropped := p.writer.dropped
	p.writer.mu.Unlock()
	if dropped > 0 {
		fmt.Fprintf(os.Stderr, "ikesu pane %d: refused %d bytes of input the application did not read\n", p.ID, dropped)
	}
}
