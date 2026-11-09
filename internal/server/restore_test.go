package server

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"io"
	"os"
	"path/filepath"

	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/platform"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/tui"
)

// waitAPISocketGone polls until the api socket disappears.
func waitAPISocketGone(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "ikesu.sock")); os.IsNotExist(err) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("api socket never went away")
}

// loadTestConfig parses $IKESU_CONFIG for tests.
func loadTestConfig(t *testing.T) (config.Config, error) {
	t.Helper()
	return config.Load()
}

// newTestServer builds a bare Server for unit tests: model state + app +
// supervisor, no sockets. db() resolves through $IKESU_STATE_DIR.
func newTestServer(t *testing.T, session string) *Server {
	t.Helper()
	base := t.TempDir()
	teststate.Setenv(t, "IKESU_STATE_DIR", base)
	cwd := filepath.Join(base, "cwd")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	st := model.New(cwd, 80, 24)
	sup := runtime.NewSupervisor()
	app := tui.NewApp(st, sup, io.Discard, cwd)
	return &Server{
		receiptClock: wallReceiptClock{},
		session:      session,
		dir:          filepath.Join(base, session),
		app:          app,
		sup:          sup,
	}
}

// reportAgent sends a pane.report_agent call, failing the test on error.
func reportAgent(t *testing.T, dir, paneID, agent, sessionID string) {
	t.Helper()
	var ok api.OkResult
	params := api.PaneReportAgentParams{
		PaneID: paneID, Source: "test", Agent: agent, State: "working",
	}
	if sessionID != "" {
		params.SessionID = &sessionID
	}
	apiOK(t, dir, api.MethodPaneReportAgent, params, &ok)
}

// G3 exact restore: two panes in the same workspace (same cwd) report
// different session ids; after server.restart both panes come back as
// shells and agent.restore --dry-run plans each pane's own id — never a
// shared cwd-latest guess.
func TestAgentRestoreDryRunDistinctIDs(t *testing.T) {
	teststate.Lock(t)
	base, err := os.MkdirTemp("/tmp", "ikesutest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "IKESU_STATE_DIR", base)
	teststate.Setenv(t, "SHELL", "/bin/sh")

	dir, done := startServerAt(t, "rest")

	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "right", Focus: true}, nil)
	ids := paneIDs(t, dir)
	if len(ids) != 2 {
		t.Fatalf("panes = %v, want 2", ids)
	}
	reportAgent(t, dir, ids[0], "claude", "sess-alpha")
	reportAgent(t, dir, ids[1], "codex", "sess-beta")

	// The ledger must hold one row per pane with its own id.
	store, err := dispatch.OpenReadOnly(dispatch.DBPath("rest"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.PaneSessions("rest")
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if len(rows) != 2 {
		t.Fatalf("ledger rows = %v, want 2", rows)
	}
	seen := map[string]string{}
	for _, r := range rows {
		seen[r.SessionID] = r.Agent
	}
	if seen["sess-alpha"] != "claude" || seen["sess-beta"] != "codex" {
		t.Fatalf("ledger = %v", rows)
	}

	// session.json must carry the per-pane ids too — the snapshot and the
	// ledger both keep the exact association.
	f := waitForSessionFile(t, dir)
	gotIDs := map[int]string{}
	f.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) {
		gotIDs[p.ID] = p.SessionID
	})
	if gotIDs[1] != "sess-alpha" || gotIDs[2] != "sess-beta" {
		t.Fatalf("session.json pane ids = %v", gotIDs)
	}

	// Restart: panes come back as shells carrying their records.
	apiOK(t, dir, api.MethodServerRestart, api.EmptyParams{}, nil)
	waitAPISocketGone(t, dir)
	waitServerExit(t, done) // the old Run must fully release the session lock first
	dir2, done2 := startServerAt(t, "rest")
	done = done2
	defer func() { apiOK(t, dir2, api.MethodServerStop, api.EmptyParams{}, nil); waitServerExit(t, done) }()

	var res api.AgentRestoreResult
	apiOK(t, dir2, api.MethodAgentRestore, api.AgentRestoreParams{DryRun: true}, &res)
	if len(res.Actions) != 2 {
		t.Fatalf("actions = %+v, want 2", res.Actions)
	}
	byPane := map[string]api.AgentRestoreActionInfo{}
	for _, a := range res.Actions {
		byPane[a.PaneID] = a
		if a.Status != "would_launch" {
			t.Fatalf("action %s status = %q, want would_launch", a.PaneID, a.Status)
		}
	}
	want := map[string]string{
		ids[0]: "claude --resume sess-alpha",
		ids[1]: "codex resume sess-beta",
	}
	for pane, cmd := range want {
		a := byPane[pane]
		if a.Command == nil || *a.Command != cmd {
			t.Fatalf("pane %s command = %v, want %q", pane, a.Command, cmd)
		}
	}
}

// G3 fail-closed: a pane whose record (e.g. loaded from session.json)
// carries an unsafe session id is skipped, never resumed as another
// conversation.
func TestAgentRestoreSkipsUnsafeID(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "w")
	pid := s.app.State().Workspaces[0].Tabs[0].Layout.Focused()
	s.app.RecordSession(pid, tui.AgentRecord{
		Agent: "claude", Kind: "id", Value: "last", Source: "restore",
	})
	res := s.runAgentRestore(true)
	if len(res.Actions) != 1 {
		t.Fatalf("actions = %+v, want 1", res.Actions)
	}
	a := res.Actions[0]
	if a.Status != "skipped" {
		t.Fatalf("status = %q, want skipped", a.Status)
	}
	if a.Reason == nil || *a.Reason != "no resumable session found" {
		t.Fatalf("reason = %v", a.Reason)
	}
	if a.Command != nil {
		t.Fatalf("command = %v, want nil (fail closed)", *a.Command)
	}
}

func TestAgentRestoreSkipsPaneWithoutProcess(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "missing-process")
	pid := s.app.State().Workspaces[0].Tabs[0].Layout.Focused()
	rec := tui.AgentRecord{Agent: "claude", Kind: "id", Value: "sess-missing", Source: "restore"}
	s.app.RecordSession(pid, rec)
	s.ledgerPut(pid, rec)
	s.ledgerWG.Wait()
	result := s.runAgentRestore(false)
	if len(result.Actions) != 1 || result.Actions[0].Status != "skipped" || result.Actions[0].Reason == nil || *result.Actions[0].Reason != "pane process not running" || result.Actions[0].Command != nil {
		t.Fatalf("missing process action = %+v", result.Actions)
	}
	if entry, ok := s.ledgerEntry(pid); !ok || entry.SessionID != rec.Value {
		t.Fatalf("ledger lost: %+v, %v", entry, ok)
	}
}

// --- unit: command construction -------------------------------------------

func TestBuildResumeCommand(t *testing.T) {
	s := &Server{}
	cmd, ok := s.buildResumeCommand("claude", "id", "abc-123")
	if !ok || cmd != "claude --resume abc-123" {
		t.Fatalf("claude builtin = %q ok=%v", cmd, ok)
	}
	cmd, ok = s.buildResumeCommand("codex", "id", "019ef3a2-749c-7b52-b324-2c20cb0b2379")
	if !ok || cmd != "codex resume 019ef3a2-749c-7b52-b324-2c20cb0b2379" {
		t.Fatalf("codex builtin = %q ok=%v", cmd, ok)
	}
	if _, ok := s.buildResumeCommand("mystery", "id", "x"); ok {
		t.Fatal("unknown agent must fail closed without a template")
	}
}

// [agent_start.commands].codex argv is preserved verbatim, with resume
// and the pane's own id appended.
func TestBuildResumeCommandAgentStartArgv(t *testing.T) {
	teststate.Lock(t)
	cfgFile := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgFile, []byte(`
[agent_start.commands]
codex = ["codex", "--sandbox", "workspace-write", "--model", "gpt-6-astra"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "IKESU_CONFIG", cfgFile)
	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg}
	cmd, ok := s.buildResumeCommand("codex", "id", "sid-9")
	want := "command codex --sandbox workspace-write --model gpt-6-astra resume sid-9"
	if !ok || cmd != want {
		t.Fatalf("cmd = %q ok=%v want %q", cmd, ok, want)
	}
}

// A template carrying a standalone --last token can never resume the
// exact session — it is rejected even with a valid {session_id}.
func TestBuildResumeCommandRejectsLastToken(t *testing.T) {
	teststate.Lock(t)
	cfgFile := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgFile, []byte(`
[agent_restore.templates]
codex = "codex resume {session_id} --last"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "IKESU_CONFIG", cfgFile)
	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg}
	if cmd, ok := s.buildResumeCommand("codex", "id", "sid-1"); ok {
		t.Fatalf("template with --last produced %q", cmd)
	}
}

func TestBuildResumeCommandRejectsLastInConfiguredArgv(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[agent_start.commands]\ncodex = [\"codex\", \"--last\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "IKESU_CONFIG", path)
	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg}
	if command, ok := s.buildResumeCommand("codex", "id", "sid-exact"); ok {
		t.Fatalf("configured --last must not compete with exact id: %q", command)
	}
}

// --- unit: cmdline observation --------------------------------------------

func TestObserveCmdline(t *testing.T) {
	job := platform.ForegroundJob{ProcessGroupID: 1, Processes: []platform.ForegroundProcess{
		{PID: 1, Name: "claude", Argv: []string{"claude", "--resume", "sess-old"}},
	}}
	if id, ok := session.ObserveCmdline(job); !ok || id != "sess-old" {
		t.Fatalf("got %q ok=%v", id, ok)
	}
	job.Processes[0].Argv = []string{"claude", "--resume=sess-eq"}
	if id, ok := session.ObserveCmdline(job); !ok || id != "sess-eq" {
		t.Fatalf("got %q ok=%v", id, ok)
	}
	job.Processes[0].Argv = []string{"codex", "resume", "sess-sub"}
	if id, ok := session.ObserveCmdline(job); !ok || id != "sess-sub" {
		t.Fatalf("got %q ok=%v", id, ok)
	}
	// Conflicting ids across processes are ambiguous → fail closed.
	job.Processes = append(job.Processes, platform.ForegroundProcess{
		PID: 2, Name: "codex", Argv: []string{"codex", "resume", "sess-other"},
	})
	if _, ok := session.ObserveCmdline(job); ok {
		t.Fatal("conflicting ids must not resolve")
	}
	// Plain launches carry no resume shape.
	job.Processes = job.Processes[:1]
	job.Processes[0].Argv = []string{"claude"}
	if _, ok := session.ObserveCmdline(job); ok {
		t.Fatal("bare argv must not resolve")
	}
}

// --- unit: session-file observation ----------------------------------------

func writeJSONL(t *testing.T, path, firstLine string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(firstLine+"\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Claude: the unique file whose first 64 lines record the pane's cwd and
// a timestamp near the process start is adopted; the id is the recorded
// sessionId. Two candidates resolve to nothing.
func TestObserveClaudeSessionFile(t *testing.T) {
	teststate.Lock(t)
	home := t.TempDir()
	teststate.Setenv(t, "HOME", home)
	cwd := "/Users/x/proj"
	dir := filepath.Join(home, ".claude", "projects", "-Users-x-proj")
	start := time.Now().Add(-30 * time.Second)
	ts := start.Format(time.RFC3339Nano)

	writeJSONL(t, filepath.Join(dir, "whatever-name.jsonl"),
		`{"sessionId":"sess-new","cwd":"`+cwd+`","timestamp":"`+ts+`","type":"user"}`)
	writeJSONL(t, filepath.Join(dir, "sess-old.jsonl"),
		`{"sessionId":"sess-old","cwd":"`+cwd+`","timestamp":"2020-01-01T00:00:00Z","type":"user"}`)

	id, ok := session.ObserveClaudeSessionFile(cwd, start)
	if !ok || id != "sess-new" {
		t.Fatalf("got %q ok=%v", id, ok)
	}

	// A second in-window file makes the observation ambiguous.
	writeJSONL(t, filepath.Join(dir, "sess-other.jsonl"),
		`{"sessionId":"sess-other","cwd":"`+cwd+`","timestamp":"`+ts+`","type":"user"}`)
	if _, ok := session.ObserveClaudeSessionFile(cwd, start); ok {
		t.Fatal("two in-window candidates must fail closed")
	}
	// No candidate at all.
	if _, ok := session.ObserveClaudeSessionFile(cwd, start.Add(-time.Hour)); ok {
		t.Fatal("out-of-window files must not resolve")
	}
}

// Claude: the project folder name is never the cwd evidence — files
// without a cwd field or with a different recorded cwd are not
// candidates, even when they sit in the colliding slug dir ("/a/b.c"
// and "/a/b-c" both slug to "-a-b-c").
func TestObserveClaudeSessionFileCwdStrict(t *testing.T) {
	teststate.Lock(t)
	home := t.TempDir()
	teststate.Setenv(t, "HOME", home)
	cwd := "/a/b-c" // slug "-a-b-c"
	dir := filepath.Join(home, ".claude", "projects", "-a-b-c")
	start := time.Now().Add(-30 * time.Second)
	ts := start.Format(time.RFC3339Nano)

	// Colliding sibling cwd recorded inside the same slug dir.
	writeJSONL(t, filepath.Join(dir, "dot-cwd.jsonl"),
		`{"sessionId":"sess-dot","cwd":"/a/b.c","timestamp":"`+ts+`"}`)
	// No cwd recorded at all.
	writeJSONL(t, filepath.Join(dir, "no-cwd.jsonl"),
		`{"sessionId":"sess-none","timestamp":"`+ts+`"}`)
	if _, ok := session.ObserveClaudeSessionFile(cwd, start); ok {
		t.Fatal("files without a matching recorded cwd must not be candidates")
	}
	if _, ok := session.ObserveClaudeSessionFile("/a/b.c", start); !ok {
		t.Fatal("the /a/b.c record should resolve for cwd /a/b.c")
	}

	// Once the real cwd's file exists, it alone resolves.
	writeJSONL(t, filepath.Join(dir, "real.jsonl"),
		`{"sessionId":"sess-real","cwd":"`+cwd+`","timestamp":"`+ts+`"}`)
	id, ok := session.ObserveClaudeSessionFile(cwd, start)
	if !ok || id != "sess-real" {
		t.Fatalf("got %q ok=%v", id, ok)
	}
}

// Codex fail-closed: a session_meta without payload.cwd, with a foreign
// cwd, or with a stale timestamp never counts as a candidate; only
// payload.session_id is used when payload.id is absent.
func TestObserveCodexSessionFileStrict(t *testing.T) {
	teststate.Lock(t)
	home := t.TempDir()
	teststate.Setenv(t, "HOME", home)
	cwd := "/Users/x/proj"
	start := time.Now().Add(-30 * time.Second)
	ts := start.Format(time.RFC3339Nano)
	day := filepath.Join(home, ".codex", "sessions",
		start.Format("2006"), start.Format("01"), start.Format("02"))
	rollout := func(suffix, payload string) {
		writeJSONL(t, filepath.Join(day, "rollout-"+start.Format("2006-01-02T15-04-05")+"-"+suffix+".jsonl"),
			`{"timestamp":"`+ts+`","type":"session_meta","payload":{`+payload+`}}`)
	}

	rollout("aaaa-0000-0000-0000-000000000001", `"id":"nocwd","timestamp":"`+ts+`"`)                               // no cwd
	rollout("aaaa-0000-0000-0000-000000000002", `"id":"other","timestamp":"`+ts+`","cwd":"/else/where"`)           // wrong cwd
	rollout("aaaa-0000-0000-0000-000000000003", `"id":"stale","timestamp":"2020-01-01T00:00:00Z","cwd":"`+cwd+`"`) // stale ts
	if _, ok := session.ObserveCodexSessionFile(cwd, start); ok {
		t.Fatal("rollouts without a matching payload.cwd must not be candidates")
	}
	writeJSONL(t, filepath.Join(day, "rollout-wrong-type.jsonl"),
		`{"type":"event_msg","label":"session_meta","payload":{"id":"wrong-type","timestamp":"`+ts+`","cwd":"`+cwd+`"}}`)
	if _, ok := session.ObserveCodexSessionFile(cwd, start); ok {
		t.Fatal("a session_meta mention outside the record type must not identify a session")
	}

	// Indeterminate in-window files prevent adopting the otherwise valid id.
	// Exclude these files by mtime before checking the session_id fallback.
	for _, name := range []string{"rollout-" + start.Format("2006-01-02T15-04-05") + "-aaaa-0000-0000-0000-000000000001.jsonl", "rollout-wrong-type.jsonl"} {
		old := start.Add(-121 * time.Second)
		if err := os.Chtimes(filepath.Join(day, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	// payload.session_id is the fallback when payload.id is absent.
	rollout("aaaa-0000-0000-0000-000000000004", `"session_id":"sess-fb","timestamp":"`+ts+`","cwd":"`+cwd+`"`)
	id, ok := session.ObserveCodexSessionFile(cwd, start)
	if !ok || id != "sess-fb" {
		t.Fatalf("session_id fallback: got %q ok=%v", id, ok)
	}
}

// --- unit: ledger ----------------------------------------------------------

func TestPaneSessionLedger(t *testing.T) {
	teststate.Lock(t)
	base := t.TempDir()
	teststate.Setenv(t, "IKESU_STATE_DIR", base)
	store, err := dispatch.Open(dispatch.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	rec := dispatch.PaneSession{
		Session: "s1", PaneID: 3, TerminalID: "p3", WorkspaceID: 1,
		TabID: 2, Cwd: "/tmp/x", Agent: "claude", SessionKind: "id",
		SessionID: "sess-1", Title: "task", Source: "report:test",
		ObservedAt: "2026-10-08T00:00:00Z",
	}
	if err := store.PutPaneSession(rec); err != nil {
		t.Fatal(err)
	}
	rows, err := store.PaneSessions("s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SessionID != "sess-1" || rows[0].Agent != "claude" {
		t.Fatalf("rows = %+v", rows)
	}
	// Other sessions' panes are separate.
	other, _ := store.PaneSessions("s2")
	if len(other) != 0 {
		t.Fatalf("s2 rows = %+v", other)
	}
	if err := store.DeletePaneSession("s1", 3); err != nil {
		t.Fatal(err)
	}
	rows, _ = store.PaneSessions("s1")
	if len(rows) != 0 {
		t.Fatalf("after delete rows = %+v", rows)
	}
}

// --- unit: warnings ----------------------------------------------------------

// sessionWarnings reports agent panes lacking a safe session id, with the
// spec's reason wording.
func TestAgentSessionWarnings(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "w")

	pid := s.app.State().Workspaces[0].Tabs[0].Layout.Focused()
	// Agent pane with no id anywhere → missing session id.
	s.app.RecordSession(pid, tui.AgentRecord{Agent: "claude", Source: "report:test"})
	w := s.agentSessionWarnings()
	if len(w) != 1 || w[0].Reason != "missing session id" || w[0].Agent != "claude" {
		t.Fatalf("warnings = %+v", w)
	}
	// Unsafe terminal id → invalid terminal session id.
	s.app.RecordSession(pid, tui.AgentRecord{Agent: "claude", Kind: "id", Value: "last", Source: "report:test"})
	w = s.agentSessionWarnings()
	if len(w) != 1 || w[0].Reason != "invalid terminal session id" {
		t.Fatalf("warnings = %+v", w)
	}
	// Safe id → no warning.
	s.app.RecordSession(pid, tui.AgentRecord{Agent: "claude", Kind: "id", Value: "sess-ok", Source: "report:test"})
	if w := s.agentSessionWarnings(); len(w) != 0 {
		t.Fatalf("warnings = %+v", w)
	}
}

// --- retention / cleanup ----------------------------------------------------

// paneExit types `exit` into the pane's shell and waits for the exit to
// be processed (the pane either stays as a respawned shell or is gone).
func paneExit(t *testing.T, dir, paneID string) {
	t.Helper()
	var ok api.OkResult
	apiOK(t, dir, api.MethodPaneSendInput,
		api.PaneSendInputParams{PaneID: paneID, Text: "exit", Keys: []string{"enter"}}, &ok)
}

// SPEC G3 fossil expiry + stale authority clearing, end to end: a pane
// labelled claude whose screen shows a frozen spinner drops to idle
// after ~15s, and a reported "working" authority over an explicitly
// idle Claude screen is cleared after ~15s.
