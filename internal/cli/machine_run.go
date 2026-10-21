package cli

import (
	"fmt"
	"os"

	"github.com/kazuph/ikesu/internal/machine"
)

func (r *runner) remoteRun(ref string, sync bool, argv []string, cwd string, pane bool) ([]string, error) {
	p, err := machine.Resolve(r.catalog().Load(), ref, true)
	if err != nil {
		return nil, err
	}
	local, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	directory := machine.RemoteDirectory(local, home, cwd)
	ssh := append([]string{"ssh"}, machine.RunArgs(p, directory, argv, pane)...)
	if !sync {
		return ssh, nil
	}
	steps, err := machine.SyncPlan(p, local, directory)
	if err != nil {
		return nil, err
	}
	command := ""
	for _, step := range steps {
		command += machine.Command(step) + " && "
	}
	command += "exec " + machine.Command(ssh)
	return []string{"/bin/sh", "-c", command}, nil
}

func remoteRunError(err error) error { return fmt.Errorf("remote run: %w", err) }
