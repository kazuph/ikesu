package server

import (
	"bufio"
	"io"
	"net"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// The approved stream contract starts these clocks only after input starts.
const graphicsFrameIdle = 5 * time.Second
const graphicsFrameTotal = 30 * time.Second

type graphicsProgressReader struct {
	conn    net.Conn
	reader  io.Reader
	started time.Time
}

func (r *graphicsProgressReader) Read(data []byte) (int, error) {
	deadline := time.Time{}
	if !r.started.IsZero() {
		deadline = time.Now().Add(graphicsFrameIdle)
		if total := r.started.Add(graphicsFrameTotal); total.Before(deadline) {
			deadline = total
		}
	}
	if err := r.conn.SetReadDeadline(deadline); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

type graphicsFrameReader struct {
	progress *graphicsProgressReader
	buffer   *bufio.Reader
}

func newGraphicsFrameReader(conn net.Conn, reader *bufio.Reader) *graphicsFrameReader {
	progress := &graphicsProgressReader{conn: conn, reader: reader}
	return &graphicsFrameReader{progress: progress, buffer: bufio.NewReader(progress)}
}

func (r *graphicsFrameReader) header() (api.PaneGraphicsStreamFrame, error) {
	r.progress.started = time.Time{}
	if err := r.progress.conn.SetReadDeadline(time.Time{}); err != nil {
		return api.PaneGraphicsStreamFrame{}, err
	}
	if _, err := r.buffer.Peek(1); err != nil {
		return api.PaneGraphicsStreamFrame{}, err
	}
	r.progress.started = time.Now()
	return api.ReadGraphicsFrame(r.buffer)
}

func (r *graphicsFrameReader) body(length uint64) ([]byte, error) {
	r.progress.started = time.Now()
	return io.ReadAll(io.LimitReader(r.buffer, int64(length)))
}
