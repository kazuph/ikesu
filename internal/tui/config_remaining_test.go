package tui

import (
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigRemainingUIAndCwd(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	c, err := config.Decode([]byte(`[terminal]
new_cwd="home"
shell_mode="non_login"
default_shell="/bin/sh"
[ui]
prompt_new_tab_name=false
sidebar_collapsed_mode="hidden"
sidebar_start_collapsed=true
mouse_scroll_lines=5
copy_on_select=false
[ui.toast.clipboard]
enabled=false
[keys]
navigate_workspace_up="u"
navigate_workspace_down="d"
`))
	if err != nil || len(c.Warnings) > 0 {
		t.Fatal(c.Warnings, err)
	}
	a.SetSidebarConfig(c, true)
	if a.st.SidebarArea().W != 0 {
		t.Fatal(a.st.SidebarArea())
	}
	specFeed(a, "\x02c")
	ws := a.st.ActiveWorkspace()
	p := a.sup.Get(ws.ActiveLayout().Focused())
	if a.dialog != nil || p == nil || p.SpawnCwd() != home {
		t.Fatal(a.dialog, p)
	}
	a.newWorkspace()
	if a.st.ActiveWorkspace().Cwd != home {
		t.Fatal(a.st.ActiveWorkspace())
	}
	a.workspaceNavigate = true
	a.navigateSelected = a.st.Active
	specFeed(a, "u")
	if a.navigateSelected != 0 {
		t.Fatal(a.navigateSelected)
	}
	a.copied = "Copied 1 lines"
	if a.copiedFlash() != "" {
		t.Fatal("disabled clipboard feedback")
	}
	c.Terminal.NewCwd = "~/fixed"
	a.SetSidebarConfig(c, false)
	if a.newPaneCwd(a.cwd) != filepath.Join(os.Getenv("HOME"), "fixed") {
		t.Fatal(a.newPaneCwd(a.cwd))
	}
	a.exec(model.CmdSelectWorkspace{Index: 0})
}

func TestConfigUnsupportedWarningUsesTopBanner(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	c, err := config.Decode([]byte("onboarding=false\n[update]\nversion_check=true\n"))
	if err != nil || len(c.Warnings) != 1 {
		t.Fatal(err, c.Warnings)
	}
	a.SetSidebarConfig(c, false)
	if !a.configWarningBanner || a.configWarning != "config.toml: update.version_check unsupported; gtx config check" {
		t.Fatal(a.configWarning, a.configWarningBanner)
	}
}
