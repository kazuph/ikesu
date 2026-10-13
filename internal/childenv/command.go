package childenv

import (
	"context"
	"os"
	"os/exec"
)

// Command starts a distinct child context. Callers may append deliberate
// environment extras or inject fresh pane guard state afterwards.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = Scrub(os.Environ())
	return cmd
}
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = Scrub(os.Environ())
	return cmd
}
