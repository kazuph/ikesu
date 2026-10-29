package guard

import (
	"fmt"
	"os"
	"path/filepath"
)

type Transfer struct {
	Root, Pane, Bin, MarkerPath, Executable, Active string
	Config                                          Config
	Meta                                            Marker
	Injected, Prompt                                bool
	Mode, Unsupported, Issue                        string
	Native                                          executableIdentity
}

func (a *Assets) Transfer() Transfer {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Transfer{Root: a.Root, Pane: a.Pane, Bin: a.Bin, MarkerPath: a.MarkerPath, Executable: a.Executable, Active: a.active, Config: a.Config, Meta: a.meta, Injected: a.injected, Prompt: a.prompt, Mode: a.mode, Unsupported: a.unsupported, Issue: a.issue, Native: a.native}
}
func Import(t Transfer) *Assets {
	return &Assets{Root: t.Root, Pane: t.Pane, Bin: t.Bin, MarkerPath: t.MarkerPath, Executable: t.Executable, native: t.Native, active: t.Active, Config: t.Config, meta: t.Meta, injected: t.Injected, prompt: t.Prompt, mode: t.Mode, unsupported: t.Unsupported, issue: t.Issue}
}

// RebindSocket preserves the marker location inherited by an existing shell.
// Publication is atomic; a failed handoff can restore the old endpoint.
func (a *Assets) RebindSocket(socket string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	defer a.refreshChanges()
	if !filepath.IsAbs(socket) {
		return a.setIssue("引き継ぎ先のソケットは絶対パスで指定してください")
	}
	// Disabled and never-injected panes must remain free of guard assets.
	if !a.injected || !a.Config.Active() {
		a.meta.Socket = socket
		return nil
	}
	checkpoint, err := a.checkpointLocked()
	if err != nil {
		return err
	}
	return a.rebindSocket(checkpoint, socket)
}

func (a *Assets) rebindSocket(checkpoint *RuntimeCheckpoint, socket string) (err error) {
	defer func() {
		if err != nil {
			if restore := a.restoreCheckpointLocked(checkpoint); restore != nil {
				err = fmt.Errorf("%w; guard rollback failed: %v", err, restore)
			}
		}
	}()
	old := a.meta.Socket
	agent := a.active
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	a.Executable = exe
	info, err := os.Stat(exe)
	if err != nil {
		return err
	}
	a.native = executableFileIdentity(info)
	if err = a.publishRuntime(); err != nil {
		return err
	}
	a.meta.Socket, a.active = socket, ""
	if agent == "" {
		return nil
	}
	if err := a.sync(agent); err != nil {
		a.meta.Socket, a.active = old, agent
		return err
	}
	if a.issue != "" || !a.intact() {
		return fmt.Errorf("guard generation could not be rebound: %s", a.issue)
	}
	return nil
}
