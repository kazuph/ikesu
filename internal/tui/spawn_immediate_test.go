package tui

import (
	"path/filepath"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestNewPaneFollowsDirectoryBeforeMetadataTick(t *testing.T) {
	for _, action := range []string{"tab", "split", "configured-pane"} {
		t.Run(action, func(t *testing.T) {
			teststate.Lock(t)
			a := specRuntimeApp(t)
			if err := a.StartInitialPane(); err != nil {
				t.Fatal(err)
			}
			p := a.sup.Get(a.focusedPane())
			next, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.WriteToPTY([]byte("cd '" + next + "'; printf '%s%s\\n' FOLLOW_NOW_ READY\n")); err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "FOLLOW_NOW_READY")
			switch action {
			case "tab":
				a.exec(model.CmdNewTab{Workspace: 0})
			case "split":
				a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
			case "configured-pane":
				a.runKeyCommand(config.PluginKeyBinding{Type: "pane", Command: "exec /bin/sh"})
			}
			created := a.sup.Get(a.focusedPane())
			if created == nil || created == p {
				t.Fatal("new pane absent")
			}
			if got := created.SpawnCwd(); got != next {
				t.Fatalf("immediate directory=%s, want %s", got, next)
			}
		})
	}
}
