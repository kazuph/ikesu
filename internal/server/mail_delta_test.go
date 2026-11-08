package server

import (
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/teststate"

	"testing"
)

func TestDuplicateAgentSendPersistsFailureAndExactRecipient(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)
	first := firstPaneID(t, dir)
	var split api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right"}, &split)
	second := split.Pane.PaneID
	for _, pane := range []string{first, second} {
		reportAgent(t, dir, pane, "codex", "")
		label := "same-name"
		apiOK(t, dir, api.MethodPaneRename, api.PaneRenameParams{PaneID: pane, Label: &label}, nil)
		apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: pane, Source: "test", Agent: "codex", State: "blocked"}, nil)
	}
	response := apiReq(t, dir, api.MethodMsgSend, api.MsgSendParams{Room: "delta", FromAgent: "sender", To: "same-name", Body: "keep original"})
	if response.Err == nil || response.Err.Code != api.ErrAmbiguous {
		t.Fatal(response)
	}
	response = apiReq(t, dir, api.MethodMsgSend, api.MsgSendParams{Room: "delta", FromAgent: "sender", To: first, Body: "exact recipient"})
	if response.Err != nil {
		t.Fatal(response.Err)
	}
	var sent api.MsgSendResult
	json.Unmarshal(response.Result, &sent)
	if len(sent.Messages) != 1 || sent.Messages[0].ToAgent != first {
		t.Fatal(sent)
	}
	store := openStore(t)
	rows, err := store.History("delta", nil, 10)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	pending, _ := store.PendingMessagesForAgent("delta", "same-name")
	if len(pending) != 0 {
		t.Fatal("ambiguous mail queued")
	}
	for _, to := range []string{"same-name", "missing", "p999999"} {
		res := apiReq(t, dir, api.MethodMsgSend, api.MsgSendParams{Room: dispatch.JobsRoom, FromAgent: dispatch.RunShellActor, To: to, Body: "completion body"})
		if res.Err == nil {
			t.Fatal("job name or missing pane accepted", to)
		}
	}
	pending, _ = store.PendingJobMessages()
	if len(pending) != 0 {
		t.Fatal(pending)
	}
	res := apiReq(t, dir, api.MethodRunStart, api.RunStartParams{Argv: []string{"/usr/bin/true"}})
	if res.Err == nil {
		t.Fatal("missing caller started")
	}
}

func TestMissingRunnerCancellationDoesNotReenterAppLoop(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)
	caller := firstPaneID(t, dir)
	reportAgent(t, dir, caller, "codex", "")
	apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: caller, Source: "test", Agent: "codex", State: "blocked"}, nil)
	store, err := dispatch.Open(dispatch.DBPath("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.InsertCommand("j9999", "missing", "true", "/work", caller, "duplicate", "summary", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCommandRunning("j9999", 0); err != nil {
		t.Fatal(err)
	}
	var cancelled api.RunCancelledResult
	apiOK(t, dir, api.MethodRunCancel, api.RunCancelParams{JobID: "j9999"}, &cancelled)
	if cancelled.Status != "cancelled" {
		t.Fatal(cancelled)
	}
	pending, err := store.PendingJobMessages()
	if err != nil || len(pending) != 1 || pending[0].ToAgent != caller {
		t.Fatal(pending, err)
	}
}

func TestQueuedJobMissingPaneFailsAndUnavailablePaneWaits(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "unavailable-job")
	s.mailCh = make(chan mailWrite, 4)
	s.mailInFlight = map[int64]bool{}
	s.jobInFlight = map[string]bool{}
	s.app.SetReported(1, "codex", "working")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exact, err := store.InsertMessage(dispatch.JobsRoom, "", dispatch.RunShellActor, "p1", "waiting for runtime", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	gone, err := store.InsertMessage(dispatch.JobsRoom, "", dispatch.RunShellActor, "p999999", "lost pane completion", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	s.failMissingJobWriteOnLoop(mailWrite{pane: 1, room: dispatch.JobsRoom, msgIDs: []int64{exact.ID}})
	s.failMissingJobWriteOnLoop(mailWrite{pane: 999999, room: dispatch.JobsRoom, msgIDs: []int64{gone.ID}})
	var delivered bool
	if s.applyPaneMailSnapshot(1, mailQueryTarget{generation: s.app.State().PaneGeneration(1)}, s.readPaneMail(1), &delivered) ||
		s.applyPaneMailSnapshot(999999, mailQueryTarget{}, s.readPaneMail(999999), &delivered) || delivered || len(s.mailCh) != 0 {
		t.Fatal("unavailable notification delivered")
	}
	pending, err := store.PendingJobMessages()
	if err != nil || len(pending) != 1 || pending[0].ID != exact.ID {
		t.Fatal(pending, err)
	}
	rows, err := store.History(dispatch.JobsRoom, nil, 10)
	if err != nil || len(rows) != 2 || rows[1].ID != gone.ID || rows[1].Body != "lost pane completion" {
		t.Fatal(rows, err)
	}
}
