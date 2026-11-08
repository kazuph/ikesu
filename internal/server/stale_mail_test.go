package server

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestOldGenerationMailAndJobAreNotQueuedForReplacement(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, dispatch.DBPathEnvVar, filepath.Join(t.TempDir(), "mail.db"))
	state := model.New(t.TempDir(), 80, 24)
	state.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	id := state.FocusedPane()
	old := state.PaneGeneration(id)
	state.Apply(model.CmdClosePane{Pane: id})
	state.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	if state.FocusedPane() != id || state.PaneGeneration(id) == old {
		t.Fatal("pane was not recreated")
	}
	sup := runtime.NewSupervisor()
	if _, err := sup.SpawnWith(id, t.TempDir(), 80, 20, nil, []string{"/bin/cat"}); err != nil {
		t.Fatal(err)
	}
	defer sup.Kill(id)
	s := &Server{sup: sup, mailCh: make(chan mailWrite, 4), mailInFlight: map[int64]bool{}, jobInFlight: map[string]bool{}}
	s.app = tui.NewApp(state, sup, io.Discard, t.TempDir())
	s.app.SetReported(id, "codex", "working")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pane := fmtPaneID(id)
	if _, err := store.InsertPaneMessage("room", "", "sender", pane, "old mail", nil, false, old); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertPaneMessage(dispatch.JobsRoom, "", dispatch.RunShellActor, pane, "old job", nil, false, old); err != nil {
		t.Fatal(err)
	}
	target := mailQueryTarget{owner: sup.Get(id), generation: state.PaneGeneration(id)}
	if s.applyPaneMailSnapshot(id, target, s.readPaneMail(id), nil) || len(s.mailCh) != 0 {
		t.Fatal("old generation was queued for delivery")
	}
	if _, err := store.InsertPaneMessage("room", "", "sender", pane, "new mail", nil, false, state.PaneGeneration(id)); err != nil {
		t.Fatal(err)
	}
	if s.applyPaneMailSnapshot(id, target, s.readPaneMail(id), nil) || len(s.mailCh) != 1 {
		t.Fatal("current generation was not queued")
	}
}
