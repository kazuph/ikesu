package tui

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/teststate"
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
