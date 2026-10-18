package server

import (
	"encoding/json"
	"io"
	"net"
	"time"
)

type ClientWriterDrained struct{ ClientID int }

type clientWrite struct {
	message clientMsg
	reply   chan clientWriteResult
}

type clientWriteResult struct {
	bytes   int
	drained *ClientWriterDrained
	err     error
}

// Render and control requests share one writer so control cannot pass a
// partially written frame. The reply itself is the server's drain event.
func runClientWriter(conn net.Conn, clientID int, render <-chan clientWrite, control <-chan clientWrite, done chan<- struct{}) {
	defer close(done)
	defer conn.Close()
	w := clientFrameWriter{conn: conn, clientID: clientID}
	write := w.write
	for {
		// Receive a queued render before selecting a control request.
		select {
		case request, ok := <-render:
			if !ok {
				render = nil
			} else if !write(request, true) {
				return
			}
			continue
		default:
		}
		select {
		case request, ok := <-render:
			if !ok {
				render = nil
			} else if !write(request, true) {
				return
			}
		case request, ok := <-control:
			if ok {
				if !write(request, false) {
					return
				}
				continue
			}
			w.finishAfterControlClose(render)
			return
		}
	}
}

type clientFrameWriter struct {
	conn     net.Conn
	clientID int
}

func (w clientFrameWriter) write(request clientWrite, frame bool) bool {
	conn := w.conn
	data, err := json.Marshal(request.message)
	size := len(data) + 1
	if err == nil {
		data = append(data, '\n')
		_ = conn.SetWriteDeadline(time.Now().Add(clientWriteTimeout))
		for len(data) > 0 {
			var n int
			n, err = conn.Write(data)
			data = data[n:]
			if err != nil {
				break
			}
			if n == 0 {
				err = io.ErrShortWrite
				break
			}
		}
		_ = conn.SetWriteDeadline(time.Time{})
	}
	result := clientWriteResult{err: err, bytes: size}
	if frame && err == nil {
		result.drained = &ClientWriterDrained{ClientID: w.clientID}
	}
	request.reply <- result
	return err == nil
}

func (w clientFrameWriter) finishAfterControlClose(render <-chan clientWrite) {
	// SPEC's close path allows a final render for 5ms.
	timer := time.NewTimer(5 * time.Millisecond)
	defer timer.Stop()
	select {
	case request, ok := <-render:
		if ok {
			w.write(request, true)
		}
	case <-timer.C:
	}
}
