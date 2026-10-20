package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestConfigDefaultShellAndModeReachRealPTY(t *testing.T) {
	teststate.Lock(t)
	// An executable shell fixture prints its actual startup arguments. This is
	// the real process selected by config and launched on the production PTY.
	dir := t.TempDir()
	shell := filepath.Join(dir, "configured-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'CONFIG_SHELL_ARGS=%s\\n' \"$*\"\nexec /bin/cat\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"login", "non_login", "auto"} {
		t.Run(mode, func(t *testing.T) {
			a := specRuntimeApp(t)
			c, err := config.Decode([]byte(fmt.Sprintf("[terminal]\ndefault_shell=%q\nshell_mode=%q\n", shell, mode)))
			if err != nil || len(c.Warnings) > 0 {
				t.Fatal(c.Warnings, err)
			}
			a.SetSidebarConfig(c, false)
			if err := a.StartInitialPane(); err != nil {
				t.Fatal(err)
			}
			want := "CONFIG_SHELL_ARGS="
			if mode == "login" || mode == "auto" && runtime.GOOS == "darwin" {
				want += "-l"
			}
			specWaitText(t, a.sup.Get(a.focusedPane()), want)
			// non_login must not accidentally match the prefix of the login output.
			if mode == "non_login" || mode == "auto" && runtime.GOOS != "darwin" {
				text := a.sup.Get(a.focusedPane()).ScreenText()
				if strings.Contains(text, "CONFIG_SHELL_ARGS=-l") {
					t.Fatal(text)
				}
			}
		})
	}
}

func TestConfigWorktreeDirectoryCreatesCheckoutInConfiguredRoot(t *testing.T) {
	teststate.Lock(t)
	a, _ := worktreeApp(t)
	root := filepath.Join(t.TempDir(), "configured-worktrees")
	c, err := config.Decode([]byte(fmt.Sprintf("[worktrees]\ndirectory=%q\n", root)))
	if err != nil {
		t.Fatal(err)
	}
	a.SetSidebarConfig(c, false)
	branch := "config-directory"
	ws := fmt.Sprintf("s%d", a.st.ActiveWorkspace().ID)
	_, err = a.CreateWorktree(api.WorktreeCreateParams{WorkspaceID: &ws, Branch: &branch, Focus: true})
	if err != nil {
		t.Fatal(err)
	}
	path := a.st.ActiveWorkspace().Cwd
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		t.Fatal(path, root, err)
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSelectWorkspace{Index: 0})
}
