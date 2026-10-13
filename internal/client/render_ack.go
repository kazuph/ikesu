package client

import "errors"

// Only ACK EPIPE is a half-close: output errors, input write failures and
// protocol errors retain their existing failure paths.
type closedRenderAck struct{ error }

func (e *closedRenderAck) Unwrap() error { return e.error }

func isClosedRenderAck(err error) bool {
	var closed *closedRenderAck
	return errors.As(err, &closed)
}
