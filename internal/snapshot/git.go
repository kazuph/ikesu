package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/childenv"
)

type Runner func(ctx context.Context, dir string, args ...string) (string, error)

type GitInfo struct {
	Branch    string
	Status    string
	DiffStat  string
	Log       string
	Truncated string
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := childenv.CommandContextGroup(ctx, "git", append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-C", dir}, args...)...)
	cmd.WaitDelay = 500 * time.Millisecond
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	if args[0] == "status" {
		var status statusSummary
		cmd.Stdout = &status
		err := cmd.Run()
		if errors.Is(err, exec.ErrWaitDelay) {
			_ = cmd.Cancel()
		}
		return status.String(), err
	}
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrWaitDelay) {
		_ = cmd.Cancel()
	}
	return string(out), err
}

const statusLimit = 50

// Keep the first rows, then count newline boundaries without buffering the
// omitted contents. A partial row at EOF still counts as one entry.
type statusSummary struct {
	first   strings.Builder
	rows    int
	partial bool
}

func (s *statusSummary) Write(data []byte) (int, error) {
	size := len(data)
	for len(data) > 0 && s.rows < statusLimit {
		newline := bytes.IndexByte(data, '\n')
		if newline < 0 {
			s.first.Write(data)
			s.partial = true
			return size, nil
		}
		s.first.Write(data[:newline+1])
		s.rows++
		s.partial = false
		data = data[newline+1:]
	}
	if len(data) > 0 {
		s.rows += bytes.Count(data, []byte{'\n'})
		s.partial = data[len(data)-1] != '\n'
	}
	return size, nil
}

func (s *statusSummary) String() string {
	rows := s.rows
	if s.partial {
		rows++
	}
	text := strings.TrimRight(s.first.String(), "\r\n")
	if rows > statusLimit {
		text += fmt.Sprintf("\nほか %d 件", rows-statusLimit)
	}
	return text
}

func CollectGit(ctx context.Context, dir string, run Runner) *GitInfo {
	if dir == "" {
		return nil
	}
	injected := run != nil
	if !injected {
		run = runGit
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	inside, err := run(ctx, dir, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return nil
	}

	var info GitInfo
	commands := []struct {
		args []string
		out  *string
	}{
		{[]string{"rev-parse", "--abbrev-ref", "HEAD"}, &info.Branch},
		{[]string{"status", "--short"}, &info.Status},
		{[]string{"diff", "--stat"}, &info.DiffStat},
		{[]string{"log", "-n", "3", "--oneline"}, &info.Log},
	}
	for _, command := range commands {
		text, err := run(ctx, dir, command.args...)
		if err == nil {
			if injected && command.args[0] == "status" {
				var status statusSummary
				_, _ = io.WriteString(&status, text)
				text = status.String()
			}
			lines := strings.Split(strings.TrimRight(text, "\r\n"), "\n")
			for i, line := range lines {
				lines[i] = cleanText(line)
			}
			*command.out = strings.Join(redactLines(lines), "\n")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			info.Truncated = "git timeout"
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	return &info
}
