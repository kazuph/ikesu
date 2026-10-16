package tui

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
	"strings"
	"testing"
)

func TestStage2ResizeExit(t *testing.T) {
	teststate.Lock(t)
	for _, key := range []string{"\r", "r", "\x1b[114;9u"} {
		t.Run(key, func(t *testing.T) {
			a := specRuntimeApp(t)
			c, err := config.Decode([]byte("[keys]\nprefix='ctrl+t'\nresize_mode=['prefix+r','cmd+r']\n"))
			if err != nil {
				t.Fatal(err)
			}
			a.SetSidebarConfig(c, false)
			specFeed(a, "\x14r")
			if !a.resizeMode {
				t.Fatal("resize did not start")
			}
			specFeed(a, key)
			if a.resizeMode {
				t.Fatal("resize did not end")
			}
		})
	}
}

func TestStage2NavigateVisibleNumber(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.newWorkspace()
	a.st.Workspaces[1].Section = "favorite"
	a.workspaceNavigate = true
	a.navigateSelected = 0
	specFeed(a, "1")
	if a.st.Active != 1 || a.workspaceNavigate {
		t.Fatal(a.st.Active, a.workspaceNavigate)
	}
}

func TestStage2NavigateCustom(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	c, err := config.Decode([]byte("[keys]\nprefix='ctrl+t'\n[[keys.command]]\nkey='prefix+g'\ntype='pane'\ncommand='exec /bin/cat'\n"))
	if err != nil {
		t.Fatal(err)
	}
	a.SetSidebarConfig(c, false)
	specFeed(a, "\x14w")
	before := a.focusedPane()
	specFeed(a, "g")
	if a.navigator != nil || a.workspaceNavigate || a.focusedPane() == before {
		t.Fatal("navigate custom command not executed", a.navigator, a.workspaceNavigate, a.focusedPane(), before)
	}
}

func TestStage2NavigateExitConditions(t *testing.T) {
	teststate.Lock(t)
	for _, row := range []struct {
		key  string
		stay bool
	}{{"\t", true}, {"h", true}, {"n", false}, {"z", false}, {"b", false}, {"v", false}} {
		t.Run(row.key, func(t *testing.T) {
			a := specRuntimeApp(t)
			c, err := config.Decode([]byte("[keys]\nprefix='ctrl+t'\n"))
			if err != nil {
				t.Fatal(err)
			}
			a.SetSidebarConfig(c, false)
			specFeed(a, "\x14v\x14w")
			specFeed(a, row.key)
			if a.workspaceNavigate != row.stay {
				t.Fatal("navigate persistence", a.workspaceNavigate, row.stay)
			}
		})
	}
}

// The distribution targets the displayed workspace, despite the initial ledger.
func TestStage2NavigateDisplayedWorkspaceDialogs(t *testing.T) {
	teststate.Lock(t)
	for _, key := range []string{"W", "D"} {
		t.Run(key, func(t *testing.T) {
			a := specRuntimeApp(t)
			c, err := config.Decode([]byte("[keys]\nprefix='ctrl+t'\n"))
			if err != nil {
				t.Fatal(err)
			}
			a.SetSidebarConfig(c, false)
			a.exec(model.CmdRenameWorkspace{Index: 0, Name: "first"})
			a.newWorkspace()
			a.exec(model.CmdRenameWorkspace{Index: 1, Name: "second"})
			a.exec(model.CmdSelectWorkspace{Index: 0})
			specFeed(a, "\x14w\x1b[B")
			if a.navigateSelected != 1 || a.st.Active != 0 {
				t.Fatal("selection fixture", a.navigateSelected, a.st.Active)
			}
			specFeed(a, key)
			if a.dialog == nil {
				t.Fatal("dialog missing")
			}
			if key == "W" && a.dialog.d.Input != "first" {
				t.Fatal(a.dialog.d.Input)
			}
			if key == "D" && !strings.HasPrefix(a.dialog.d.Detail, "first —") {
				t.Fatal(a.dialog.d.Detail)
			}
		})
	}
}
