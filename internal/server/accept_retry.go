package server

import (
	"errors"
	"net"
	"os"
	"time"
)

type deadlineListener interface {
	SetDeadline(time.Time) error
}

// acceptWithRetry keeps a listener alive across temporary failures such as
// running out of file descriptors (EMFILE). Only a closed listener ends it;
// before 2026-11-09 a single error stopped API or display connections for
// good while the server kept running.
//
// After a failure the readiness of connections already waiting may have been
// consumed, so the retry uses a deadline to try the accept again by itself
// instead of waiting for the next client to connect.
func acceptWithRetry(ln net.Listener, stopped func() bool) (net.Conn, error) {
	delay := 5 * time.Millisecond
	retrying := false
	for {
		conn, err := ln.Accept()
		if err == nil {
			if retrying {
				if d, ok := ln.(deadlineListener); ok {
					_ = d.SetDeadline(time.Time{})
				}
			}
			return conn, nil
		}
		if errors.Is(err, net.ErrClosed) || stopped() {
			return nil, err
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			if d, ok := ln.(deadlineListener); ok {
				_ = d.SetDeadline(time.Now().Add(delay))
			}
			continue
		}
		retrying = true
		time.Sleep(delay)
		if delay < time.Second {
			delay *= 2
		}
		if d, ok := ln.(deadlineListener); ok {
			_ = d.SetDeadline(time.Now().Add(delay))
		}
	}
}
