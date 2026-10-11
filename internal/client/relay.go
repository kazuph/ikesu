package client

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	"golang.org/x/sys/unix"

	"github.com/kazuph/goterrace/internal/api"
)

// Relay is the private SSH transport entry point. It only dials an already
// running server and forwards the endpoint protocol without interpreting it.
func Relay(session string, in, out *os.File) error {
	if !api.ValidSessionName(session) {
		return fmt.Errorf("invalid endpoint session name")
	}
	conn, err := dialClient(api.SessionDir(session))
	if err != nil {
		return fmt.Errorf("remote endpoint server unavailable: %w", err)
	}
	defer conn.Close()
	// Inherited stdin may lack Go's poller. An owned pollable duplicate
	// lets server EOF interrupt the input read without waiting for SSH EOF.
	fd, err := unix.Dup(int(in.Fd()))
	if err != nil {
		return err
	}
	unix.CloseOnExec(fd)
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return err
	}
	input := os.NewFile(uintptr(fd), "endpoint-stdin")
	defer input.Close()
	inputDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(conn, input)
		if half, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = half.CloseWrite()
		}
		inputDone <- err
	}()
	_, outputErr := io.Copy(out, conn)
	// This process owns stdin exclusively. Closing it releases a blocked read
	// when the server closes before the SSH client supplies EOF.
	_ = input.Close()
	_ = in.Close()
	_ = conn.Close()
	inputErr := <-inputDone
	if outputErr != nil {
		return outputErr
	}
	if inputErr != nil && !errors.Is(inputErr, os.ErrClosed) && !errors.Is(inputErr, net.ErrClosed) {
		return inputErr
	}
	return nil
}
