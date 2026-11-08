package server

import (
	"bufio"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func graphicsReaderSocket(t *testing.T) (net.Conn, net.Conn, *graphicsFrameReader) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "tb-frame-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	listener, err := net.Listen("unix", filepath.Join(root, "frame.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	peer, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { peer.Close() })
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	reader := newGraphicsFrameReader(conn, bufio.NewReader(conn))
	if reader.progress.idle != graphicsFrameIdle || reader.progress.total != graphicsFrameTotal {
		t.Fatalf("production deadlines: idle=%v total=%v", reader.progress.idle, reader.progress.total)
	}
	reader.progress.idle = 200 * time.Millisecond
	reader.progress.total = time.Second
	return conn, peer, reader
}

func TestGraphicsFrameReaderRejectsStalledHeaderAndBody(t *testing.T) {
	t.Parallel()
	for _, body := range []bool{false, true} {
		name := "header"
		if body {
			name = "body"
		}
		t.Run(name, func(t *testing.T) {
			_, peer, reader := graphicsReaderSocket(t)
			wire := "{"
			if body {
				wire = "{\"data_length\":3}\na"
			}
			if _, err := peer.Write([]byte(wire)); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			_, err := reader.header()
			if body && err == nil {
				var data []byte
				data, err = reader.body(3)
				if string(data) != "a" {
					t.Fatal("partial body was changed", string(data))
				}
			}
			var timeout net.Error
			if !errors.As(err, &timeout) || !timeout.Timeout() || time.Since(start) < reader.progress.idle || time.Since(start) > reader.progress.idle*2 {
				t.Fatalf("stalled %s error=%v elapsed=%v", name, err, time.Since(start))
			}
		})
	}
}

func TestGraphicsFrameReaderTotalDeadlineDespiteProgress(t *testing.T) {
	t.Parallel()
	_, peer, reader := graphicsReaderSocket(t)
	if _, err := peer.Write([]byte("{")); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(reader.progress.idle / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := peer.Write([]byte(" ")); err != nil {
					return
				}
			case <-stop:
				return
			}
		}
	}()
	start := time.Now()
	_, err := reader.header()
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() || time.Since(start) < reader.progress.total || time.Since(start) > reader.progress.total+reader.progress.idle {
		t.Fatalf("progress bypassed total deadline: error=%v elapsed=%v", err, time.Since(start))
	}
}
