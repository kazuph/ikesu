package server

import (
	"bufio"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func writerSocketPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	var connections [2]net.Conn
	for i, fd := range fds {
		file := os.NewFile(uintptr(fd), "writer-pair")
		connections[i], err = net.FileConn(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { connections[i].Close() })
	}
	return connections[0], connections[1]
}

func TestG4RenderWriteThenDrainedBothReceivePaths(t *testing.T) {
	for _, closing := range []bool{false, true} {
		server, client := writerSocketPair(t)
		render := make(chan clientWrite, 1)
		control := make(chan clientWrite)
		done := make(chan struct{})
		request := clientWrite{message: clientMsg{Type: "output", Data: encode64([]byte("FRAME"))}, reply: make(chan clientWriteResult)}
		render <- request
		if closing {
			// Directly exercise the same 5ms receiver used after control close.
			go func() { clientFrameWriter{conn: server, clientID: 7}.finishAfterControlClose(render); close(done) }()
		} else {
			go runClientWriter(server, 7, render, control, done)
		}
		client.SetReadDeadline(time.Now().Add(5 * time.Second))
		message, err := readClientMsg(bufio.NewReader(client))
		if err != nil || message.Type != "output" || message.Data != request.message.Data {
			t.Fatal(closing, message, err)
		}
		select {
		case result := <-request.reply:
			if result.err != nil || result.drained == nil || result.drained.ClientID != 7 {
				t.Fatal(closing, result)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing drained event")
		}
		if !closing {
			close(control)
		}
		<-done
	}
}

func TestG4WriteFailureNeverDrainedBothReceivePaths(t *testing.T) {
	for _, closing := range []bool{false, true} {
		server, client := writerSocketPair(t)
		client.Close()
		render := make(chan clientWrite, 1)
		control := make(chan clientWrite)
		done := make(chan struct{})
		request := clientWrite{message: clientMsg{Type: "output", Data: "FRAME"}, reply: make(chan clientWriteResult, 1)}
		render <- request
		if closing {
			go func() { clientFrameWriter{conn: server, clientID: 7}.finishAfterControlClose(render); close(done) }()
		} else {
			go runClientWriter(server, 7, render, control, done)
		}
		select {
		case result := <-request.reply:
			if result.err == nil || result.drained != nil {
				t.Fatal("failed write drained", closing, result)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("writer did not stop")
		}
		<-done
	}
}
