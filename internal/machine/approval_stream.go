package machine

import (
	"bytes"
	"crypto/rand"
	"io"
	"sync"
	"time"
)

const ApprovalWait = 30 * time.Minute

// ApprovalStream consumes only its private connection marker; every original
// stderr byte (including the full authentication URL) reaches the job log.
type RunApproval struct {
	mu                          sync.Mutex
	ID, Marker                  string
	Expire                      func()
	timer                       *time.Timer
	deadlineAt                  time.Time
	connected, awaiting, closed bool
}

type ApprovalStream struct {
	State   *RunApproval
	Out     io.Writer
	pending []byte
}

func PrepareRunApproval(profiles []Profile, argv []string, expire func()) (*RunApproval, []string) {
	marker := "ikesu-ssh-connected-" + rand.Text()
	id, observed := ObserveRun(profiles, argv, marker)
	if id == "" {
		return nil, argv
	}
	return &RunApproval{ID: id, Marker: marker, Expire: expire}, observed
}

func (s *RunApproval) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
	}
}

func (s *ApprovalStream) Write(p []byte) (int, error) {
	s.State.mu.Lock()
	defer s.State.mu.Unlock()
	if s.State.connected && len(s.pending) == 0 {
		return s.Out.Write(p)
	}
	s.pending = append(s.pending, p...)
	for {
		if s.State.connected {
			_, err := s.Out.Write(s.pending)
			s.pending = nil
			if err != nil {
				return 0, err
			}
			break
		}
		at := bytes.IndexByte(s.pending, '\n')
		if at < 0 {
			break
		}
		line := s.pending[:at+1]
		if err := s.line(line); err != nil {
			return 0, err
		}
		s.pending = s.pending[at+1:]
	}
	return len(p), nil
}

func (s *ApprovalStream) line(line []byte) error {
	state := s.State
	if !state.connected && !state.closed && string(bytes.TrimRight(line, "\r\n")) == state.Marker {
		state.connected = true
		if state.timer != nil {
			state.timer.Stop()
		}
		return ClearApproval(state.ID)
	}
	if _, err := s.Out.Write(line); err != nil {
		return err
	}
	if !state.connected && !state.awaiting && !state.closed {
		_, url, ok := bytes.Cut(line, []byte("To authenticate, visit:"))
		url = bytes.TrimSpace(url)
		if ok && len(url) > 0 {
			state.awaiting = true
			state.deadlineAt = time.Now().Add(ApprovalWait)
			state.timer = time.AfterFunc(ApprovalWait, func() { state.expire(time.Now()) })
			if err := OpenApproval(state.ID, string(url)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *ApprovalStream) Close() error {
	s.State.mu.Lock()
	defer s.State.mu.Unlock()
	if len(s.pending) > 0 {
		err := s.line(s.pending)
		s.pending = nil
		return err
	}
	return nil
}

func (s *RunApproval) expire(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.connected && !s.closed && s.awaiting && !now.Before(s.deadlineAt) {
		s.Expire()
	}
}
