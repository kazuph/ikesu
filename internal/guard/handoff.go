package guard

import (
	"os"
	"path/filepath"
)

type Transfer struct {
	Root, Pane, Bin, MarkerPath, Executable, Active string
	Config                                          Config
	Meta                                            Marker
	Injected, Prompt                                bool
	Mode, Unsupported, Issue                        string
}

func (a *Assets) Transfer() Transfer {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Transfer{a.Root, a.Pane, a.Bin, a.MarkerPath, a.Executable, a.active, a.Config, a.meta, a.injected, a.prompt, a.mode, a.unsupported, a.issue}
}
func Import(t Transfer) *Assets {
	return &Assets{Root: t.Root, Pane: t.Pane, Bin: t.Bin, MarkerPath: t.MarkerPath, Executable: t.Executable, active: t.Active, Config: t.Config, meta: t.Meta, injected: t.Injected, prompt: t.Prompt, mode: t.Mode, unsupported: t.Unsupported, issue: t.Issue}
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
	old := a.meta.Socket
	agent := a.active
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	a.Executable = exe
	if err = a.publishRuntime(); err != nil {
		return err
	}
	if err = a.refreshStartupFiles(); err != nil {
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
	return nil
}
