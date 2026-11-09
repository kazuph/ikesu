package runtime

import (
	"errors"
	"sync"
)

// ptyInputLimit bounds input queued for an application that stopped reading.
// Writes beyond it are refused instead of growing without limit.
const ptyInputLimit = 8 << 20

// ErrPTYInputFull reports that the application has not read its queued input.
var ErrPTYInputFull = errors.New("pane input not delivered: the application is not reading its input")

type ptyWrite struct {
	data []byte
	done chan ptyWriteResult
}

type ptyWriteResult struct {
	n   int
	err error
}

// ptyWriter is the only writer of a pane's PTY. Callers append to its queue
// and never block on the PTY themselves, so an application that stops reading
// its input cannot stall the app loop or anyone holding a pane lock
// (2026-11-09: a full PTY froze the server for eight hours).
type ptyWriter struct {
	mu      sync.Mutex
	queue   []ptyWrite
	queued  int
	stopped error
	ready   chan struct{}
	once    sync.Once
}

func (w *ptyWriter) init() {
	w.once.Do(func() { w.ready = make(chan struct{}, 1) })
}

// enqueue adds data without waiting. done, when set, receives the result.
func (w *ptyWriter) enqueue(data []byte, done chan ptyWriteResult) error {
	w.init()
	w.mu.Lock()
	if w.stopped != nil {
		err := w.stopped
		w.mu.Unlock()
		return err
	}
	if w.queued+len(data) > ptyInputLimit {
		w.mu.Unlock()
		return ErrPTYInputFull
	}
	w.queue = append(w.queue, ptyWrite{append([]byte(nil), data...), done})
	w.queued += len(data)
	w.mu.Unlock()
	select {
	case w.ready <- struct{}{}:
	default:
	}
	return nil
}

func (w *ptyWriter) stop(err error) {
	w.mu.Lock()
	if w.stopped == nil {
		w.stopped = err
	}
	pending := w.queue
	w.queue, w.queued = nil, 0
	w.mu.Unlock()
	for _, item := range pending {
		if item.done != nil {
			item.done <- ptyWriteResult{0, err}
		}
	}
}

// run writes queued input in order until the PTY fails or the pane ends.
func (p *Pane) runPTYWriter() {
	w := &p.writer
	w.init()
	for {
		select {
		case <-w.ready:
		case <-p.done:
			w.stop(errPaneClosed)
			return
		}
		for {
			w.mu.Lock()
			if len(w.queue) == 0 {
				w.mu.Unlock()
				break
			}
			item := w.queue[0]
			w.queue = w.queue[1:]
			w.mu.Unlock()
			n, err := writeAllPTY(p.currentPTY(), item.data)
			w.mu.Lock()
			w.queued -= len(item.data)
			w.mu.Unlock()
			if item.done != nil {
				item.done <- ptyWriteResult{n, err}
			}
			if err != nil {
				w.stop(err)
				return
			}
		}
	}
}

var errPaneClosed = errors.New("pane closed: input not delivered")
