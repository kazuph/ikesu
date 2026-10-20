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

// StartLoginPath starts exactly one lookup; it never waits for the shell.
func (s *Supervisor) StartLoginPath() {
	s.loginPathOnce.Do(func() {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
		}
		timeout := loginShellPATHTimeout
		go func() {
			defer close(s.loginPathDone)
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			reader, writer, err := os.Pipe()
			if err != nil {
				s.loginPathErr = fmt.Errorf("login shell PATH: %w", err)
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
			result := <-readDone
			out := result.data
			if err == nil {
				err = result.err
			}
			if ctx.Err() != nil {
				err = ctx.Err()
				if cmd.Process != nil {
					_ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
				}
			}
			if err != nil {
				s.loginPathErr = fmt.Errorf("login shell PATH: %w", err)
				return
			}
			parts := strings.Split(string(out), "\x00")
			if len(parts) < 3 || parts[len(parts)-2] == "" {
				s.loginPathErr = fmt.Errorf("login shell returned no PATH")
				return
			}
			s.loginPath = parts[len(parts)-2]
		}()
	})
}

// WaitLoginPath must be called outside the app loop.
func (s *Supervisor) WaitLoginPath() error {
	s.StartLoginPath()
	select {
	case <-s.loginPathDone:
		return s.loginPathErr
	case <-s.done:
		return fmt.Errorf("login shell PATH: server stopped")
	}
}

// LoginPath only reads a completed lookup, so app-loop callers never wait.
func (s *Supervisor) LoginPath() (string, error) {
	s.StartLoginPath()
	select {
	case <-s.loginPathDone:
		return s.loginPath, s.loginPathErr
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
