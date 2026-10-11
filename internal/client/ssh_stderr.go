package client

import (
	"bufio"
	"io"
	"strings"
)

// REVIEW1 requests only the last 64 KiB of SSH diagnostics, independently of
// approval URL parsing. A ring avoids copying the retained tail on every line.
const sshStderrLimit = 64 * 1024
const sshApprovalPrefix = "To authenticate, visit:"

// Ordinary diagnostics never accumulate an unbounded unterminated line.
// Only a detected approval URL is assembled separately for exact delivery.
func (s *sshEndpoint) readStderr(input io.Reader) {
	rd := bufio.NewReader(input)
	var url strings.Builder
	search := ""
	found := false
	for {
		fragment, err := rd.ReadSlice('\n')
		s.mu.Lock()
		s.stderr.WriteString(string(fragment))
		s.mu.Unlock()
		if found {
			url.Write(fragment)
		} else {
			search += string(fragment)
			if _, value, ok := strings.Cut(search, sshApprovalPrefix); ok {
				found = true
				url.WriteString(value)
			} else if len(search) >= len(sshApprovalPrefix) {
				search = search[len(search)-len(sshApprovalPrefix)+1:]
			}
		}
		if err != bufio.ErrBufferFull {
			if found {
				s.noteApprovalURL(strings.TrimSpace(url.String()))
			}
			url.Reset()
			search, found = "", false
			if err != nil {
				return
			}
		}
	}
}

type sshStderrTail struct {
	data        []byte
	start, size int
}

func (b *sshStderrTail) WriteString(text string) {
	if b.data == nil {
		b.data = make([]byte, sshStderrLimit)
	}
	if len(text) >= sshStderrLimit {
		copy(b.data, text[len(text)-sshStderrLimit:])
		b.start, b.size = 0, sshStderrLimit
		return
	}
	pos := (b.start + b.size) % sshStderrLimit
	n := copy(b.data[pos:], text)
	copy(b.data, text[n:])
	if overflow := b.size + len(text) - sshStderrLimit; overflow > 0 {
		b.start = (b.start + overflow) % sshStderrLimit
	}
	b.size = min(b.size+len(text), sshStderrLimit)
}

func (b *sshStderrTail) String() string {
	if b.size == 0 {
		return ""
	}
	end := min(b.start+b.size, len(b.data))
	return string(b.data[b.start:end]) + string(b.data[:b.size-(end-b.start)])
}
