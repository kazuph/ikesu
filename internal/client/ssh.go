package client

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/machine"
)

// sshEndpoint owns only the SSH process it started. The remote helper dials
// an existing socket, so retries cannot start or stop a remote server.
type sshEndpoint struct {
	cmd                      *exec.Cmd
	in                       io.WriteCloser
	out                      io.ReadCloser
	done                     chan struct{}
	onApproval               func(string)
	mu                       sync.Mutex
	stderr                   sshStderrTail
	err                      error
	deadline                 *time.Timer
	deadlineAt               time.Time
	ready, approval, expired bool
	closeOnce                sync.Once
}

func openSSHEndpoint(profile machine.Profile, approval func(string)) (*sshEndpoint, error) {
	c, err := config.Load()
	if err != nil {
		return nil, err
	}
	managed, err := machine.PrepareSSH(c, profile.Target)
	if err != nil {
		return nil, err
	}
	started := false
	defer func() {
		if !started {
			managed.Close()
		}
	}()
	binary := os.Getenv("GTX_MACHINE_REMOTE_BINARY")
	if binary == "" {
		binary = "gtx"
	}
	cmd := exec.Command("ssh", managed.Args(machine.ForwardArgs(profile, binary, []string{"__endpoint"}))...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, output, err := os.Pipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	cmd.Stdout = output
	stderr, err := cmd.StderrPipe()
	if err != nil {
		in.Close()
		out.Close()
		output.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		in.Close()
		out.Close()
		output.Close()
		stderr.Close()
		return nil, err
	}
	output.Close()
	started = true
	s := &sshEndpoint{cmd: cmd, in: in, out: out, done: make(chan struct{}), onApproval: approval}
	s.deadlineAt = time.Now().Add(sshConnectWait)
	s.deadline = time.AfterFunc(sshConnectWait, s.timeout)
	go func() {
		s.readStderr(stderr)
		err := cmd.Wait()
		s.mu.Lock()
		s.deadline.Stop()
		s.err = err
		s.mu.Unlock()
		managed.Close()
		close(s.done)
	}()
	return s, nil
}

func approvalURL(line string) string {
	_, url, found := strings.Cut(line, sshApprovalPrefix)
	if !found {
		return ""
	}
	return strings.TrimSpace(url)
}

func (s *sshEndpoint) noteStderr(line string) {
	s.mu.Lock()
	s.stderr.WriteString(line)
	s.mu.Unlock()
	s.noteApprovalURL(approvalURL(line))
}

func (s *sshEndpoint) noteApprovalURL(url string) {
	s.mu.Lock()
	first := url != "" && !s.approval && !s.ready && !s.expired
	if first {
		s.approval = true
		s.deadlineAt = time.Now().Add(sshApprovalWait)
		s.deadline.Reset(sshApprovalWait)
	}
	s.mu.Unlock()
	if first && s.onApproval != nil {
		s.onApproval(url)
	}
}

func (s *sshEndpoint) timeout() {
	s.mu.Lock()
	if s.ready {
		s.mu.Unlock()
		return
	}
	if remaining := time.Until(s.deadlineAt); remaining > 0 {
		s.deadline.Reset(remaining)
		s.mu.Unlock()
		return
	}
	s.expired = true
	s.mu.Unlock()
	_ = s.cmd.Process.Kill()
}

func (s *sshEndpoint) connected() {
	s.mu.Lock()
	s.ready = true
	s.deadline.Stop()
	s.mu.Unlock()
}

func (s *sshEndpoint) Read(p []byte) (int, error) {
	n, err := s.out.Read(p)
	if err == nil || !errors.Is(err, io.EOF) {
		return n, err
	}
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.approval && (s.expired || !s.ready) {
		return n, fmt.Errorf("SSH approval expired: %s", strings.TrimSpace(s.stderr.String()))
	}
	if s.expired {
		return n, fmt.Errorf("SSH connection timed out after %s: %s", sshConnectWait, strings.TrimSpace(s.stderr.String()))
	}
	if s.err != nil {
		return n, fmt.Errorf("ssh: %w: %s", s.err, strings.TrimSpace(s.stderr.String()))
	}
	return n, err
}

func (s *sshEndpoint) Write(p []byte) (int, error) { return s.in.Write(p) }

func (s *sshEndpoint) Close() error {
	s.closeOnce.Do(func() {
		s.in.Close()
		_ = s.cmd.Process.Kill()
		s.out.Close()
		<-s.done
	})
	return nil
}
