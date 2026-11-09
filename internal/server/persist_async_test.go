package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/platform"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestSessionSnapshotKeepsEachPaneLedgerID(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "ledger-snapshot")
	s.app.State().Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	ids := s.app.State().Workspaces[0].Tabs[0].Layout.PaneIDs()
	if len(ids) != 2 {
		t.Fatal(ids)
	}
	for i, pid := range ids {
		s.ledgerPut(pid, tui.AgentRecord{Agent: "codex", Kind: "id", Value: []string{"first-id", "second-id"}[i], Source: "report:test"})
	}
	s.ledgerWG.Wait()
	if s.store != nil {
		defer s.store.Close()
	}
	snapshot := s.captureSessionWithoutObservation()
	if snapshot == nil {
		t.Fatal("missing snapshot")
	}
	got := map[int]string{}
	snapshot.file.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) { got[p.ID] = p.SessionID })
	if got[ids[0]] != "first-id" || got[ids[1]] != "second-id" {
		t.Fatalf("pane ledger IDs changed: %v", got)
	}
}

func TestDelayedSessionObservationPreservesExplicitReport(t *testing.T) {
	teststate.Lock(t)
	root := t.TempDir()
	teststate.Setenv(t, "CODEX_HOME", root)
	s := newTestServer(t, "delayed-observation")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	defer func() {
		s.saveMu.Lock()
		if s.saveTimer != nil {
			s.saveTimer.Stop()
		}
		s.saveMu.Unlock()
	}()
	pid := s.app.State().Workspaces[0].Tabs[0].Layout.Focused()
	p := s.sup.Get(pid)
	p.SetAgent("codex")
	job := p.ForegroundJob()
	start, ok := session.ProcessStart(job)
	if !ok {
		t.Fatal("could not read real foreground process start")
	}
	path := filepath.Join(root, "sessions", "rollout-test.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{
		"id": "observed-id", "cwd": p.Snapshot().Cwd, "timestamp": start.Format(time.RFC3339Nano),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	observation, ok := s.prepareSessionObservation(pid)
	if !ok {
		t.Fatal("could not prepare observation")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	job = platform.ForegroundJob{Processes: []platform.ForegroundProcess{{PID: os.Getpid()}}}
	id, found := session.ObserveProcessSession("codex", p.Snapshot().Cwd, start, job, []string{"CODEX_HOME=" + root})
	if !found || id != "observed-id" {
		t.Fatalf("real open session file was not observed: %q %v", id, found)
	}
	observation.found = true
	observation.result = tui.AgentRecord{Agent: "codex", Kind: "id", Value: id, Source: "observed:file"}
	reported := tui.AgentRecord{Agent: "codex", Kind: "id", Value: "reported-id", Source: "report:test"}
	s.app.RecordSession(pid, reported)
	s.applySessionObservation(observation)
	if got, _ := s.app.SessionRecord(pid); got != reported {
		t.Fatalf("delayed observation replaced the explicit report: %+v", got)
	}
}

func TestSessionObservationRejectsDuplicatePaneID(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "duplicate-observation")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	pid := s.app.State().Workspaces[0].Tabs[0].Layout.Focused()
	p := s.sup.Get(pid)
	p.SetAgent("codex")
	if resp := s.paneSplit("test", api.PaneSplitParams{Direction: "right"}); resp.Err != nil {
		t.Fatal(resp.Err)
	}
	ids := s.app.State().Workspaces[0].Tabs[0].Layout.PaneIDs()
	if len(ids) != 2 || s.sup.Get(ids[1]) == nil {
		t.Fatal("second PTY was not started", ids)
	}
	s.app.RecordSession(ids[1], tui.AgentRecord{Agent: "codex", Kind: "id", Value: "shared-id", Source: "report:test"})
	o, ok := s.prepareSessionObservation(pid)
	if !ok {
		t.Fatal("missing observation")
	}
	o.found = true
	o.result = tui.AgentRecord{Agent: "codex", Kind: "id", Value: "shared-id", Source: "observed:file"}
	s.applySessionObservation(o)
	if rec, ok := s.app.SessionRecord(pid); ok && rec.Value == "shared-id" {
		t.Fatalf("duplicate accepted: %+v", rec)
	}
}

func TestReportedDuplicateCodexSessionIsNotRecorded(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "duplicate-report")
	defer s.ledgerWG.Wait()
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	pid := s.app.State().Workspaces[0].Tabs[0].Layout.Focused()
	if resp := s.paneSplit("test", api.PaneSplitParams{Direction: "right"}); resp.Err != nil {
		t.Fatal(resp.Err)
	}
	ids := s.app.State().Workspaces[0].Tabs[0].Layout.PaneIDs()
	if len(ids) != 2 || s.sup.Get(ids[1]) == nil {
		t.Fatal("second PTY was not started", ids)
	}
	s.app.RecordSession(ids[1], tui.AgentRecord{Agent: "codex", Kind: "id", Value: "shared-id", Source: "report:test"})
	id := "shared-id"
	response := s.paneReportAgent("test", api.PaneReportAgentParams{PaneID: fmtPaneID(pid), Agent: "codex", Source: "test", SessionID: &id})
	if response.Err == nil {
		t.Fatal("duplicate report accepted")
	}
	if rec, ok := s.app.SessionRecord(pid); ok && rec.Value == id {
		t.Fatalf("duplicate recorded: %+v", rec)
	}
}

func TestScheduledSessionCoalescesOverlappingChanges(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "coalesced-save")
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.app.Run(); close(done) }()
	defer func() { s.app.Stop(); <-done }()
	s.saveWriteMu.Lock()
	locked := true
	defer func() {
		if locked {
			s.saveWriteMu.Unlock()
		}
	}()
	first := make(chan struct{})
	go func() { s.saveScheduledSession(); close(first) }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		var sequence uint64
		s.app.Do(func() { sequence = s.saveSequence })
		if sequence != 0 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("first snapshot was not captured")
		default:
		}
	}
	s.app.Do(func() {
		s.app.State().Workspaces[0].Name = "LATEST"
		s.markSessionDirty()
	})
	s.saveScheduledSession()
	s.saveWriteMu.Unlock()
	locked = false
	select {
	case <-first:
	case <-deadline.C:
		t.Fatal("first write did not finish")
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		f, err := session.Load(session.Path(s.dir))
		if err == nil && f != nil && f.Workspaces[0].Name == "LATEST" {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("coalesced write did not save the latest state", err)
		case <-tick.C:
		}
	}
}

func TestSessionWriteOrderFinalSaveAndFailurePreserveNewestSnapshot(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "ordered-save")
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		t.Fatal(err)
	}
	s.app.State().Workspaces[0].Name = "OLDER"
	older := s.captureSession()
	s.app.State().Workspaces[0].Name = "FINAL"
	s.saveSession()
	s.writeSession(older)
	check := func(want string) {
		t.Helper()
		f, err := session.Load(session.Path(s.dir))
		if err != nil || f == nil || f.Workspaces[0].Name != want {
			t.Fatal("delayed write replaced the final state", f, err)
		}
	}
	check("FINAL")
	s.app.State().Workspaces[0].Name = "RETRY"
	retry := s.captureSession()
	previous := s.savedSequence
	if err := os.Rename(s.dir, s.dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.dir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.writeSession(retry)
	if s.savedSequence != previous {
		t.Fatal("failed filesystem write advanced the committed generation")
	}
	if err := os.Remove(s.dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(s.dir+"-saved", s.dir); err != nil {
		t.Fatal(err)
	}
	check("FINAL")
	s.writeSession(retry)
	check("RETRY")
	s.app.State().Workspaces[0].Name = "RETIRED"
	retired := s.captureSession()
	s.closeSessionWrites()
	s.writeSession(retired)
	check("RETRY")
}
