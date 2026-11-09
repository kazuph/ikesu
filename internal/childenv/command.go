package childenv

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// Command starts a distinct child context. Callers may append deliberate
// environment extras or inject fresh pane guard state afterwards.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = Scrub(os.Environ())
	return cmd
}

// CommandContextGroup cancels a command and its child processes together.
func CommandContextGroup(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	return cmd
}
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = Scrub(os.Environ())
	return cmd
}
