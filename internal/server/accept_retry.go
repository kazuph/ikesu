package server

import (
	"errors"
	"net"
	"time"
)

// acceptWithRetry keeps a listener alive across temporary failures such as
// running out of file descriptors (EMFILE). Only a closed listener ends it;
// before 2026-11-09 a single error stopped API or display connections for
// good while the server kept running.
func acceptWithRetry(ln net.Listener, stopped func() bool) (net.Conn, error) {
	delay := 5 * time.Millisecond
	for {
		conn, err := ln.Accept()
		if err == nil {
			return conn, nil
		}
		if errors.Is(err, net.ErrClosed) || stopped() {
			return nil, err
		}
		time.Sleep(delay)
		if delay < time.Second {
			delay *= 2
		}
	}
}
