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
	return conn, peer, newGraphicsFrameReader(conn, bufio.NewReader(conn))
}

func TestGraphicsFrameReaderPreservesCoalescedBodyAndNextHeader(t *testing.T) {
	_, peer, reader := graphicsReaderSocket(t)
	if _, err := peer.Write([]byte("{\"data_length\":3}\nabc{\"sequence\":7,\"revision\":8}\n")); err != nil {
		t.Fatal(err)
	}
	first, err := reader.header()
	if err != nil || first.DataLength == nil || *first.DataLength != 3 {
		t.Fatal(first, err)
	}
	body, err := reader.body(3)
	if err != nil || string(body) != "abc" {
		t.Fatal("body changed", string(body), err)
	}
	second, err := reader.header()
	if err != nil || second.Sequence != 7 || second.Revision != 8 {
		t.Fatal("prefetched next header was lost", second, err)
	}
}

func TestGraphicsFrameReaderAllowsUnstartedIdle(t *testing.T) {
	conn, peer, reader := graphicsReaderSocket(t)
	// A previous frame's deadline must not leak into the idle phase.
	conn.SetReadDeadline(time.Now())
	done := make(chan error, 1)
	go func() {
		frame, err := reader.header()
		if err == nil && frame.Sequence != 7 {
			err = errors.New("header sequence changed")
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("unstarted frame was closed: %v", err)
	case <-time.After(graphicsFrameIdle + time.Second):
	}
	if _, err := peer.Write([]byte("{\"sequence\":7}\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(graphicsFrameIdle):
		t.Fatal("idle frame could not resume")
	}
}

func TestGraphicsFrameReaderRejectsStalledHeaderAndBody(t *testing.T) {
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
			if !errors.As(err, &timeout) || !timeout.Timeout() || time.Since(start) < graphicsFrameIdle || time.Since(start) > graphicsFrameIdle*2 {
				t.Fatalf("stalled %s error=%v elapsed=%v", name, err, time.Since(start))
			}
		})
	}
}

func TestGraphicsFrameReaderTotalDeadlineDespiteProgress(t *testing.T) {
	_, peer, reader := graphicsReaderSocket(t)
	if _, err := peer.Write([]byte("{")); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(graphicsFrameIdle / 2)
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
	if !errors.As(err, &timeout) || !timeout.Timeout() || time.Since(start) < graphicsFrameTotal || time.Since(start) > graphicsFrameTotal+graphicsFrameIdle {
		t.Fatalf("progress bypassed total deadline: error=%v elapsed=%v", err, time.Since(start))
	}
}
