package machine

import (
	"context"
	"fmt"
	"github.com/kazuph/goterrace/internal/childenv"
	"github.com/kazuph/goterrace/internal/config"
	"os"
	"path/filepath"
	"time"
)

// ManagedSSH owns a private config and control socket for one attachment.
// Include precedes fallbacks, preserving OpenSSH's first-value-wins rule.
type ManagedSSH struct {
	Dir, Target string
	Options     []string
}

func PrepareSSH(c config.Config, target string) (*ManagedSSH, error) {
	m := &ManagedSSH{Target: target}
	if c.Remote.ManageSSHConfig != nil && !*c.Remote.ManageSSHConfig {
		return m, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	m.Dir, err = os.MkdirTemp("/tmp", "gtx-ssh-")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(m.Dir, "config")
	// Values observed from herdr 0.2.24's generated configuration.
	text := fmt.Sprintf("Include %q\nInclude /etc/ssh/ssh_config\nHost *\n  ServerAliveInterval 15\n  ServerAliveCountMax 4\n", filepath.Join(home, ".ssh", "config"))
	if err = os.WriteFile(path, []byte(text), 0600); err != nil {
		os.RemoveAll(m.Dir)
		return nil, err
	}
	m.Options = []string{"-F", path, "-S", filepath.Join(m.Dir, "ctl"), "-o", "ControlMaster=auto", "-o", "ControlPersist=yes"}
	return m, nil
}
func (m *ManagedSSH) Args(args []string) []string {
	return append(append([]string{}, m.Options...), args...)
}
func (m *ManagedSSH) Close() {
	if m == nil || m.Dir == "" {
		return
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "ctl")); err == nil {
		// Match the existing SSH connection deadline; closing affects only the
		// per-attachment control master created with this private socket.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = childenv.CommandContext(ctx, "ssh", m.Args([]string{"-O", "exit", "-o", "BatchMode=yes", "--", m.Target})...).Run()
		cancel()
	}
	_ = os.RemoveAll(m.Dir)
}
