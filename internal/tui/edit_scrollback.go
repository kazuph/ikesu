package tui

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"os"
	"os/exec"
	"strings"
)

func (a *App) editScrollback() {
	pid := a.st.FocusedPane()
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
	if editor == "" {
		cmd := exec.Command("open", f.Name())
		cmd.Dir = p.SpawnCwd()
		if err = cmd.Start(); err != nil {
			os.Remove(f.Name())
			a.ShowToast("scrollback editor failed", err.Error(), "needs_attention", pid)
			return
		}
		go func() { _ = cmd.Wait() }()
	} else {
		wi, _ := a.st.FindPane(pid)
		if wi < 0 {
			os.Remove(f.Name())
			a.ShowToast("scrollback editor unsupported", "focused pane is outside the tab layout", "needs_attention", pid)
			return
		}
		a.lastSpawnError = nil
		a.exec(model.CmdSplitPane{Workspace: wi, Pane: pid, Dir: layout.Vertical, Ratio: 0.5, Focus: true,
			Cwd: p.SpawnCwd(), Argv: []string{"/bin/sh", "-c", "exec " + editor + ` "$1"`, "gtx-scrollback", f.Name()}})
		editorPane := a.st.FocusedPane()
		if a.lastSpawnError != nil || editorPane == pid {
			os.Remove(f.Name())
			if editorPane != pid {
				a.exec(model.CmdClosePane{Pane: editorPane})
			}
			a.ShowToast("scrollback editor failed", fmt.Sprint(a.lastSpawnError), "needs_attention", pid)
			return
		}
		if a.editorFiles == nil {
			a.editorFiles = map[int]string{}
		}
		a.editorFiles[editorPane] = f.Name()
		a.exec(model.CmdZoomPane{Pane: editorPane, Mode: "on"})
	}
	if !a.serverUI {
		a.ShowToast("opened scrollback", fmt.Sprintf("focused pane p%d", pid), "finished", pid)
	}
}

func (a *App) removeEditorFile(pid int) {
	if path, ok := a.editorFiles[pid]; ok {
		_ = os.Remove(path)
		delete(a.editorFiles, pid)
	}
}

func (a *App) removeEditorFiles() {
	for pid := range a.editorFiles {
		a.removeEditorFile(pid)
	}
}
