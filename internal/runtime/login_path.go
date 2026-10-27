package runtime

import (
	"context"
	"errors"
	"fmt"
	"github.com/kazuph/ikesu/internal/childenv"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Parent decision 2026-10-09: allow slow shell setup for 10 seconds because
// lookup runs outside the app loop. Tests may shorten this real-time limit.
const defaultLoginShellPATHTimeout = 10 * time.Second

var loginShellPATHTimeout = defaultLoginShellPATHTimeout

var errLoginPathPending = errors.New("login shell PATH is still being obtained")

// The brief calls for a short cooldown after failure; successful lookups remain
// cached for the supervisor lifetime. No user-facing setting is introduced.
const loginPathFailureCooldown = 5 * time.Second

type loginPathResult struct {
	done     chan struct{}
	path     string
	err      error
	failedAt time.Time
}

// StartLoginPath never waits and coalesces requests into one active lookup.
func (s *Supervisor) StartLoginPath() { s.startLoginPath() }

func (s *Supervisor) startLoginPath() *loginPathResult {
	s.loginPathMu.Lock()
	defer s.loginPathMu.Unlock()
	if result := s.loginPathResult; result != nil {
		select {
		case <-result.done:
			if result.err == nil || time.Since(result.failedAt) < loginPathFailureCooldown {
				return result
			}
		default:
			return result
		}
	}
	result := &loginPathResult{done: make(chan struct{})}
	s.loginPathResult = result
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	timeout := loginShellPATHTimeout
	go func() {
		defer func() {
			if result.err != nil {
				result.failedAt = time.Now()
			}
			close(result.done)
		}()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		reader, writer, err := os.Pipe()
		if err != nil {
			result.err = fmt.Errorf("login shell PATH: %w", err)
			return
		}
		defer reader.Close()
		defer writer.Close()
		go func() {
			select {
			case <-s.done:
				cancel()
			case <-ctx.Done():
			}
			<-ctx.Done()
			reader.Close()
		}()
		type output struct {
			data []byte
			err  error
		}
		readDone := make(chan output, 1)
		go func() { data, err := io.ReadAll(reader); readDone <- output{data, err} }()
		cmd := childenv.CommandContext(ctx, shell, "-lic", `printf '\000%s\000' "$PATH"`)
		cmd.Env = childenv.Scrub(os.Environ())
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error { return unix.Kill(-cmd.Process.Pid, unix.SIGKILL) }
		// Own the pipe so descendants holding stdout cannot extend the deadline.
		cmd.Stdout = writer
		err = cmd.Run()
		writer.Close()
		if err != nil {
			reader.Close()
		}
		readResult := <-readDone
		out := readResult.data
		if err == nil {
			err = readResult.err
		}
		if ctx.Err() != nil {
			err = ctx.Err()
			if cmd.Process != nil {
				_ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
			}
		}
		if err != nil {
			result.err = fmt.Errorf("login shell PATH: %w", err)
			return
		}
		parts := strings.Split(string(out), "\x00")
		if len(parts) < 3 || parts[len(parts)-2] == "" {
			result.err = fmt.Errorf("login shell returned no PATH")
			return
		}
		result.path = parts[len(parts)-2]
	}()
	return result
}

// WaitLoginPath must be called outside the app loop.
func (s *Supervisor) WaitLoginPath() error {
	result := s.startLoginPath()
	select {
	case <-result.done:
		return result.err
	case <-s.done:
		return fmt.Errorf("login shell PATH: server stopped")
	}
}

// LoginPath only reads a completed lookup, so app-loop callers never wait.
func (s *Supervisor) LoginPath() (string, error) {
	result := s.startLoginPath()
	select {
	case <-result.done:
		return result.path, result.err
	default:
		return "", errLoginPathPending
	}
}

func (s *Supervisor) ResolveCommand(argv []string, cwd string) ([]string, string, error) {
	if len(argv) == 0 || argv[0] == "" {
		return nil, "", fmt.Errorf("agent command is empty")
	}
	path, err := s.LoginPath()
	if err != nil {
		return nil, "", err
	}
	resolved := append([]string(nil), argv...)
	if !strings.ContainsRune(argv[0], filepath.Separator) {
		resolved[0] = ""
		for _, dir := range filepath.SplitList(path) {
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(cwd, dir)
			}
			candidate := filepath.Join(dir, argv[0])
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
				resolved[0] = candidate
				break
			}
		}
		if resolved[0] == "" {
			return nil, "", fmt.Errorf("agent command %q not found in login shell PATH", argv[0])
		}
	}
	return resolved, path, nil
}
