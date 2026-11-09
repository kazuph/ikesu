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

// ErrPTYInputStale reports queued input dropped before any of it was written
// because the condition it was accepted under no longer held.
var ErrPTYInputStale = errors.New("pane input dropped: it was no longer valid when the PTY could take it")

// ErrPTYInputTransferred reports input handed to the next server by a live
// handoff; that server writes it first.
var ErrPTYInputTransferred = errors.New("pane input transferred to the next server")

var errPaneClosed = errors.New("pane closed: input not accepted")
var errHandedOff = errors.New("pane handed off: input not accepted by this server")

// ptySegment is one accepted write. done, when set, receives nil once every
// byte is written, or the reason it was not.
type ptySegment struct {
	data    []byte
	done    chan error
	valid   func() bool
	started bool
}

// ptyWriter is the only writer of a pane's PTY. Every input path (keys,
// pastes, mouse and terminal responses from the emulator, mail, screen
// answers) appends whole writes to one ordered queue and returns at once, so
// an application that stops reading cannot stall the app loop or a lock
// holder (2026-11-09: a full PTY froze the server for eight hours). Accepted
// bytes are carried across a live handoff.
type ptyWriter struct {
	mu       sync.Mutex
	segments []ptySegment
	queued   int
	stopped  error
	paused   bool
	ready    chan struct{}
	idle     chan struct{} // closed when the running writer returns
	dropped  int
}

func (w *ptyWriter) signal() {
	select {
	case w.ready <- struct{}{}:
	default:
	}
}

// enqueue accepts all of data or none of it, without waiting. valid, when
// set, is checked before the first byte is written; a stale write is dropped.
func (w *ptyWriter) enqueue(data []byte, done chan error, valid func() bool) error {
	if len(data) == 0 {
		if done != nil {
			done <- nil
		}
		return nil
	}
	w.mu.Lock()
	switch {
	case w.stopped != nil:
		err := w.stopped
		w.mu.Unlock()
		return err
	case w.queued+len(data) > ptyInputLimit:
		w.dropped += len(data)
		w.mu.Unlock()
		return ErrPTYInputFull
	}
	w.segments = append(w.segments, ptySegment{data: append([]byte(nil), data...), done: done, valid: valid})
	w.queued += len(data)
	w.mu.Unlock()
	w.signal()
	return nil
}

// prepend puts input carried from a previous server ahead of everything.
func (w *ptyWriter) prepend(data []byte) {
	if len(data) == 0 {
		return
	}
	w.mu.Lock()
	w.segments = append([]ptySegment{{data: append([]byte(nil), data...), started: true}}, w.segments...)
	w.queued += len(data)
	w.mu.Unlock()
	w.signal()
}

// finish removes every segment and reports err to their waiters.
func (w *ptyWriter) finish(err error) []byte {
	w.mu.Lock()
	segments := w.segments
	w.segments, w.queued = nil, 0
	w.mu.Unlock()
	var bytes []byte
	for _, s := range segments {
		bytes = append(bytes, s.data...)
		if s.done != nil {
			s.done <- err
		}
	}
	return bytes
}

func (w *ptyWriter) stop(err error) {
	w.mu.Lock()
	if w.stopped == nil {
		w.stopped = err
	}
	w.mu.Unlock()
	w.finish(err)
}

// responseWriter hands emulator output (responses and encoded input) to the
// pane writer in call order. It never blocks; a refused write is counted.
type responseWriter struct{ p *Pane }

func (r responseWriter) Write(b []byte) (int, error) {
	_ = r.p.writer.enqueue(b, nil, nil)
	return len(b), nil
}

// startPTYWriter runs the writer until the PTY fails, the pane ends, or a
// handoff pauses it. Input accepted before it starts is written at once.
func (p *Pane) startPTYWriter() {
	w := &p.writer
	w.mu.Lock()
	w.paused = false
	idle := make(chan struct{})
	w.idle = idle
	w.mu.Unlock()
	w.signal()
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
				if len(w.segments) == 0 {
					w.mu.Unlock()
					break
				}
				w.mu.Unlock()
				if !p.waitWritable() {
					if p.pausing.Load() {
						continue
					}
					w.stop(errPaneClosed)
					return
				}
				p.inputMu.Lock()
				w.mu.Lock()
				if len(w.segments) == 0 {
					w.mu.Unlock()
					p.inputMu.Unlock()
					continue
				}
				head := &w.segments[0]
				if !head.started {
					if head.valid != nil && !head.valid() {
						dropped := *head
						w.segments = w.segments[1:]
						w.queued -= len(dropped.data)
						w.mu.Unlock()
						p.inputMu.Unlock()
						if dropped.done != nil {
							dropped.done <- ErrPTYInputStale
						}
						continue
					}
				}
				chunk := append([]byte(nil), head.data[:min(len(head.data), ptyWriteChunk)]...)
				w.mu.Unlock()
				written, err := writeSomePTY(p.currentPTY(), chunk)
				var finished *ptySegment
				w.mu.Lock()
				if len(w.segments) > 0 {
					head = &w.segments[0]
					if written > 0 {
						head.started = true
					}
					head.data = head.data[written:]
					w.queued -= written
					if len(head.data) == 0 {
						done := *head
						finished = &done
						w.segments = w.segments[1:]
					}
				}
				w.mu.Unlock()
				p.inputMu.Unlock()
				if finished != nil && finished.done != nil {
					finished.done <- nil
				}
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

// pauseWriterForHandoff stops the writer between chunks. Input stays queued
// (and is still accepted) until takePendingInput hands it over. The caller
// has set p.pausing and woken the PTY wait.
func (p *Pane) pauseWriterForHandoff() error {
	w := &p.writer
	w.signal()
	w.mu.Lock()
	idle := w.idle
	w.mu.Unlock()
	if idle != nil {
		select {
		case <-idle:
		case <-time.After(handoffWriterWait):
			return fmt.Errorf("pane %d: the PTY writer did not stop; handoff refused so no input is lost", p.ID)
		}
	}
	return nil
}

// takePendingInput ends this server's input for a handed-off pane and
// returns every accepted byte not yet written, in order. Later input is
// refused, so nothing is accepted that the next server would not receive.
func (p *Pane) takePendingInput() []byte {
	w := &p.writer
	w.mu.Lock()
	if w.stopped == nil {
		w.stopped = errHandedOff
	}
	w.mu.Unlock()
	return w.finish(ErrPTYInputTransferred)
}

// resumeWriterAfterHandoff restarts a writer that a refused handoff paused.
func (p *Pane) resumeWriterAfterHandoff() {
	w := &p.writer
	w.mu.Lock()
	paused := w.paused
	w.mu.Unlock()
	if paused {
		p.startPTYWriter()
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
