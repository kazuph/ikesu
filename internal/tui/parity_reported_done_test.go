package tui

import (
	"testing"

	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestParityReportedCompletionIsUnseenUntilItsPaneIsViewed(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.newWorkspace()
	pid := a.focusedPane()
	a.exec(model.CmdSelectWorkspace{Index: 0})
	a.SetReported(pid, "codex", "working")
	a.SetReported(pid, "codex", "idle")
	a.detectAll()
	if a.glyph(pid).Key != "done" {
		t.Fatal("background completion must remain unseen")
	}
	a.SetReported(pid, "codex", "idle")
	if a.glyph(pid).Key != "done" {
		t.Fatal("repeated idle reports must preserve unseen completion")
	}
	if _, state, ok := a.Reported(pid); !ok || state != "idle" {
		t.Fatal("display state must not rewrite the reported API state")
	}
	a.exec(model.CmdSelectWorkspace{Index: 1})
	a.detectAll()
	if a.glyph(pid).Key != "idle" {
		t.Fatal("viewing the completed pane must clear its unseen state")
	}
	a.exec(model.CmdSelectWorkspace{Index: 0})
	a.detectAll()
	if a.glyph(pid).Key != "idle" {
		t.Fatal("leaving the viewed pane must not recreate unseen completion")
	}
}
