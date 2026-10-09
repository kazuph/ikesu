package plugin

import (
	"fmt"
	"os/exec"
	"strings"
)

// buildOutputLimit is the per-stream tail size observed in the isolated herdr
// distribution build on 2026-10-09.
const buildOutputLimit = 65536

type outputTail struct {
	data  []byte
	total int
}

func (t *outputTail) Write(p []byte) (int, error) {
	n := len(p)
	t.total += n
	if n >= buildOutputLimit {
		t.data = append(t.data[:0], p[n-buildOutputLimit:]...)
	} else {
		if len(t.data)+n > buildOutputLimit {
			copy(t.data, t.data[len(t.data)+n-buildOutputLimit:])
			t.data = t.data[:buildOutputLimit-n]
		}
		t.data = append(t.data, p...)
	}
	return n, nil
}

func runBuild(cmd *exec.Cmd, pluginID string, index, count int) error {
	var stdout, stderr outputTail
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		status := err.Error()
		if exited, ok := err.(*exec.ExitError); ok {
			status = fmt.Sprintf("exit status: %d", exited.ExitCode())
		}
		var message strings.Builder
		fmt.Fprintf(&message, "error: plugin build failed\n  plugin: %s\n  build: %d/%d\n  cwd: %s\n  command: %s\n  status: %s\n", pluginID, index+1, count, cmd.Dir, strings.Join(cmd.Args, " "), status)
		for _, stream := range []struct {
			name   string
			output *outputTail
		}{{"stderr", &stderr}, {"stdout", &stdout}} {
			if stream.output.total == 0 {
				continue
			}
			if stream.output.total > buildOutputLimit {
				fmt.Fprintf(&message, "\n%s: showing last %d bytes; earlier output omitted\n", stream.name, buildOutputLimit)
			} else {
				fmt.Fprintf(&message, "\n%s:\n", stream.name)
			}
			message.Write(stream.output.data)
			if stream.output.data[len(stream.output.data)-1] != '\n' {
				message.WriteByte('\n')
			}
		}
		message.WriteString("\nPlugin was not installed.")
		return fmt.Errorf("%s", message.String())
	}
	return nil
}
