package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (a *App) editScrollback() {
	pid := a.focusedPane()
	p := a.sup.Get(pid)
	if p == nil {
		return
	}
	var lines []string
	for _, row := range p.BufferLines() {
		var b strings.Builder
		for _, cell := range row {
			if cell.Width > 0 {
				b.WriteString(cell.Content)
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " \t"))
	}
	text := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	f, err := os.CreateTemp("", "gtx-scrollback-*.txt")
	if err == nil {
		_, err = f.WriteString(text)
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		a.ShowToast("scrollback editor failed", err.Error(), "needs_attention", pid)
		return
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	var cmd *exec.Cmd
	if editor == "" {
		cmd = exec.Command("open", f.Name())
	} else {
		cmd = exec.Command("/bin/sh", "-c", editor+` "$1"`, "gtx-scrollback", f.Name())
	}
	cmd.Dir = p.SpawnCwd()
	if err = cmd.Start(); err != nil {
		os.Remove(f.Name())
		a.ShowToast("scrollback editor failed", err.Error(), "needs_attention", pid)
		return
	}
	go func() { _ = cmd.Wait() }()
	a.ShowToast("opened scrollback", fmt.Sprintf("focused pane p%d", pid), "finished", pid)
}
