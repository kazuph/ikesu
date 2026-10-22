// Package latency records optional diagnostics without holding up the terminal
// or session event loops. Writer failures only discard diagnostics.
package latency

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// The parent approved this per-process, per-destination diagnostics budget.
const Limit = 33554432
const LimitMessage = "ikesu_latency 上限に達したので以降の遅延記録を捨てる\n"

var Enabled = os.Getenv("IKESU_LATENCY_TRACE") == "1"

type Trace struct {
	mu      sync.Mutex
	queue   []string
	bytes   int
	stopped bool
	wake    chan struct{}
	done    chan struct{}
	writer  io.Writer
	close   io.Closer
}

func New(writer io.Writer) *Trace {
	if !Enabled {
		return nil
	}
	t := &Trace{writer: writer, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go t.run()
	return t
}

func Open(path string) *Trace {
	if !Enabled {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil
	}
	t := New(f)
	t.close = f
	return t
}

func (t *Trace) Event(event string, fields ...any) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return
	}
	line := fmt.Sprintf("ikesu_latency unix_us=%d event=%s%s\n", time.Now().UnixMicro(), event, fmt.Sprint(fields...))
	if len(line) > Limit-t.bytes {
		t.queue = append(t.queue, LimitMessage)
		t.stopped = true
	} else {
		t.bytes += len(line)
		t.queue = append(t.queue, line)
		if t.bytes == Limit {
			t.queue = append(t.queue, LimitMessage)
			t.stopped = true
		}
	}
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func (t *Trace) run() {
	defer close(t.done)
	for range t.wake {
		for {
			t.mu.Lock()
			batch := t.queue
			t.queue = nil
			stop := t.stopped
			t.mu.Unlock()
			for _, line := range batch {
				_, _ = io.WriteString(t.writer, line)
			}
			if len(batch) == 0 {
				if stop {
					return
				}
				break
			}
		}
	}
}

// Close drains queued diagnostics after the owning runtime has stopped.
func (t *Trace) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.stopped = true
	t.mu.Unlock()
	select {
	case t.wake <- struct{}{}:
	default:
	}
	<-t.done
	if t.close != nil {
		_ = t.close.Close()
	}
}
