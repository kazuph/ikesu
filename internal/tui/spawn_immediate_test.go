package tui

import (
	"path/filepath"
	"testing"

	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestSplitFollowsDirectoryBeforeMetadataTick(t *testing.T) {
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
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	created := a.sup.Get(a.focusedPane())
	if created == nil || created == p {
		t.Fatal("new pane absent")
	}
	if got := created.SpawnCwd(); got != next {
		t.Fatalf("immediate split directory=%s, want %s", got, next)
	}
}
