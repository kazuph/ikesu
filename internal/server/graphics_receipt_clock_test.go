package server

import (
	"sync"
	"time"
)

// Only the receipt deadline uses this controlled clock. Sockets, PTYs,
// encoding, image payloads and the session loop remain real.
type controlledReceiptClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []receiptTimer
}

type receiptTimer struct {
	at time.Time
	fn func()
}

func (c *controlledReceiptClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *controlledReceiptClock) AfterFunc(d time.Duration, fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timers = append(c.timers, receiptTimer{c.now.Add(d), fn})
}

func (c *controlledReceiptClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var ready []func()
	var pending []receiptTimer
	for _, timer := range c.timers {
		if !timer.at.After(c.now) {
			ready = append(ready, timer.fn)
		} else {
			pending = append(pending, timer)
		}
	}
	c.timers = pending
	c.mu.Unlock()
	for _, fn := range ready {
		fn()
	}
}
