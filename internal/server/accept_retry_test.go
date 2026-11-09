package server

import (
	"errors"
	"net"
	"syscall"
	"testing"
)

type flakyListener struct {
	net.Listener
	fails int
}

func (l *flakyListener) Accept() (net.Conn, error) {
	if l.fails > 0 {
		l.fails--
		return nil, &net.OpError{Op: "accept", Err: syscall.EMFILE}
	}
	return l.Listener.Accept()
}

// Running out of file descriptors once must not stop accepting connections.
func TestAcceptSurvivesTemporaryErrors(t *testing.T) {
	t.Parallel()
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()
	ln := &flakyListener{Listener: inner, fails: 3}
	go func() {
		if c, err := net.Dial("tcp", inner.Addr().String()); err == nil {
			defer c.Close()
		}
	}()
	conn, err := acceptWithRetry(ln, func() bool { return false })
	if err != nil {
		t.Fatalf("accept after EMFILE: %v", err)
	}
	conn.Close()
	inner.Close()
	if _, err := acceptWithRetry(ln, func() bool { return false }); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed listener must end the loop: %v", err)
	}
}
