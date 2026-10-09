package server

import (
	"bytes"
	"io"
	"sync"
)

type jobLogStream struct {
	out       io.Writer
	mu        *sync.Mutex
	prefix    string
	lineStart bool
}

// Each stream keeps its own line boundary; the shared lock preserves complete writes.
func (s *jobLogStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	written := 0
	for len(p) > 0 {
		if s.lineStart {
			if _, err := io.WriteString(s.out, s.prefix); err != nil {
				return written, err
			}
			s.lineStart = false
		}
		length := len(p)
		if at := bytes.IndexByte(p, '\n'); at >= 0 {
			length = at + 1
		}
		n, err := s.out.Write(p[:length])
		written += n
		if err != nil {
			return written, err
		}
		if n != length {
			return written, io.ErrShortWrite
		}
		s.lineStart = p[length-1] == '\n'
		p = p[length:]
	}
	return written, nil
}
