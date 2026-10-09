package server

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/teststate"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/dispatch"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/platform"
	"github.com/kazuph/goterrace/internal/runtime"
	"github.com/kazuph/goterrace/internal/session"
	"github.com/kazuph/goterrace/internal/tui"
)

// waitAPISocketGone polls until the api socket disappears.
func waitAPISocketGone(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "gtx.sock")); os.IsNotExist(err) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("api socket never went away")
}

// loadTestConfig parses $GTX_CONFIG for tests.
func loadTestConfig(t *testing.T) (config.Config, error) {
	t.Helper()
	return config.Load()
}

// newTestServer builds a bare Server for unit tests: model state + app +
// supervisor, no sockets. db() resolves through $GTX_STATE_DIR.
func newTestServer(t *testing.T, session string) *Server {
	t.Helper()
	base := t.TempDir()
	teststate.Setenv(t, "GTX_STATE_DIR", base)
	cwd := filepath.Join(base, "cwd")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	st := model.New(cwd, 80, 24)
	sup := runtime.NewSupervisor()
	app := tui.NewApp(st, sup, io.Discard, cwd)
	return &Server{
		session: session,
		dir:     filepath.Join(base, session),
		app:     app,
		sup:     sup,
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
	base, err := os.MkdirTemp("/tmp", "gtxtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "GTX_STATE_DIR", base)
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
	<-done // the old Run must fully release the session lock first
	dir2, done2 := startServerAt(t, "rest")
	done = done2
	defer func() { apiOK(t, dir2, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()

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

// G3 fail-closed: an unsafe session id reported via pane.report_agent is
// never recorded — the pane keeps no session value and the report does
// not dirty the session record.
func TestReportAgentUnsafeIDNotRecorded(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "w")
	pid := s.app.State().Workspaces[0].Tabs[0].Layout.Focused()
	bad := "bad;id"
	res := s.paneReportAgent("t", api.PaneReportAgentParams{
		PaneID: fmt.Sprintf("p%d", pid), Source: "test", Agent: "claude",
		State: "idle", SessionID: &bad,
	})
	if res.Err != nil {
		t.Fatalf("report-agent: %+v", res.Err)
	}
	if rec, ok := s.app.SessionRecord(pid); ok && rec.Value != "" {
		t.Fatalf("unsafe id recorded: %+v", rec)
	}
	if led, ok := s.ledgerEntry(pid); ok && led.SessionID != "" {
		t.Fatalf("unsafe id in ledger: %+v", led)
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

// A real (non-dry-run) restore types the resume command into the pane.
func TestAgentRestoreLaunchesCommand(t *testing.T) {
	teststate.Lock(t)
	base, err := os.MkdirTemp("/tmp", "gtxtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "GTX_STATE_DIR", base)
	teststate.Setenv(t, "SHELL", "/bin/sh")

	dir, done := startServerAt(t, "launch")
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()

	reportAgent(t, dir, "p1", "claude", "sess-launch")
	reportAgent(t, dir, "p1", "", "")

	var res api.AgentRestoreResult
	apiOK(t, dir, api.MethodAgentRestore, api.AgentRestoreParams{}, &res)
	if len(res.Actions) != 1 || res.Actions[0].Status != "launched" {
		t.Fatalf("actions = %+v, want 1 launched", res.Actions)
	}

	// The pane's screen should echo the typed resume command.
	var env api.PaneReadResultEnvelope
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		apiOK(t, dir, api.MethodPaneRead, api.PaneReadParams{PaneID: "p1", StripANSI: true}, &env)
		if strings.Contains(env.Read.Text, "claude --resume sess-launch") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("resume command never appeared; screen:\n%s", env.Read.Text)
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
	teststate.Setenv(t, "GTX_CONFIG", cfgFile)
	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg}
	cmd, ok := s.buildResumeCommand("codex", "id", "sid-9")
	want := "codex --sandbox workspace-write --model gpt-6-astra resume sid-9"
	if !ok || cmd != want {
		t.Fatalf("cmd = %q ok=%v want %q", cmd, ok, want)
	}
}

// A template missing {session_id} must not produce a command.
func TestBuildResumeCommandBadTemplate(t *testing.T) {
	teststate.Lock(t)
	cfgFile := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgFile, []byte(`
[agent_restore.templates]
claude = "claude --resume"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "GTX_CONFIG", cfgFile)
	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg}
	if cmd, ok := s.buildResumeCommand("claude", "id", "x"); ok {
		t.Fatalf("template without {session_id} produced %q", cmd)
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
	teststate.Setenv(t, "GTX_CONFIG", cfgFile)
	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg}
	if cmd, ok := s.buildResumeCommand("codex", "id", "sid-1"); ok {
		t.Fatalf("template with --last produced %q", cmd)
	}
}

// --- unit: cmdline observation --------------------------------------------

func TestObserveCmdline(t *testing.T) {
	job := platform.ForegroundJob{ProcessGroupID: 1, Processes: []platform.ForegroundProcess{
		{PID: 1, Name: "claude", Argv: []string{"claude", "--resume", "sess-old"}},
	}}
	if id, ok := observeCmdline(job); !ok || id != "sess-old" {
		t.Fatalf("got %q ok=%v", id, ok)
	}
	job.Processes[0].Argv = []string{"claude", "--resume=sess-eq"}
	if id, ok := observeCmdline(job); !ok || id != "sess-eq" {
		t.Fatalf("got %q ok=%v", id, ok)
	}
	job.Processes[0].Argv = []string{"codex", "resume", "sess-sub"}
	if id, ok := observeCmdline(job); !ok || id != "sess-sub" {
		t.Fatalf("got %q ok=%v", id, ok)
	}
	// Conflicting ids across processes are ambiguous → fail closed.
	job.Processes = append(job.Processes, platform.ForegroundProcess{
		PID: 2, Name: "codex", Argv: []string{"codex", "resume", "sess-other"},
	})
	if _, ok := observeCmdline(job); ok {
		t.Fatal("conflicting ids must not resolve")
	}
	// Plain launches carry no resume shape.
	job.Processes = job.Processes[:1]
	job.Processes[0].Argv = []string{"claude"}
	if _, ok := observeCmdline(job); ok {
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

	id, ok := observeClaudeSessionFile(cwd, start)
	if !ok || id != "sess-new" {
		t.Fatalf("got %q ok=%v", id, ok)
	}

	// A second in-window file makes the observation ambiguous.
	writeJSONL(t, filepath.Join(dir, "sess-other.jsonl"),
		`{"sessionId":"sess-other","cwd":"`+cwd+`","timestamp":"`+ts+`","type":"user"}`)
	if _, ok := observeClaudeSessionFile(cwd, start); ok {
		t.Fatal("two in-window candidates must fail closed")
	}
	// No candidate at all.
	if _, ok := observeClaudeSessionFile(cwd, start.Add(-time.Hour)); ok {
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
	if _, ok := observeClaudeSessionFile(cwd, start); ok {
		t.Fatal("files without a matching recorded cwd must not be candidates")
	}
	if _, ok := observeClaudeSessionFile("/a/b.c", start); !ok {
		t.Fatal("the /a/b.c record should resolve for cwd /a/b.c")
	}

	// Once the real cwd's file exists, it alone resolves.
	writeJSONL(t, filepath.Join(dir, "real.jsonl"),
		`{"sessionId":"sess-real","cwd":"`+cwd+`","timestamp":"`+ts+`"}`)
	id, ok := observeClaudeSessionFile(cwd, start)
	if !ok || id != "sess-real" {
		t.Fatalf("got %q ok=%v", id, ok)
	}
}

// Codex: the session_meta record decides — payload.cwd must exist and
// match, and the id is payload.id (payload.session_id fallback).
func TestObserveCodexSessionFile(t *testing.T) {
	teststate.Lock(t)
	home := t.TempDir()
	teststate.Setenv(t, "HOME", home)
	cwd := "/Users/x/proj"
	start := time.Now().Add(-30 * time.Second)
	day := filepath.Join(home, ".codex", "sessions",
		start.Format("2006"), start.Format("01"), start.Format("02"))
	name := func(suffix string) string {
		return "rollout-" + start.Format("2006-01-02T15-04-05") + "-aaaa-bbbb-cccc-dddd-" + suffix + ".jsonl"
	}
	writeJSONL(t, filepath.Join(day, name("eeee")),
		`{"timestamp":"`+start.Format(time.RFC3339Nano)+`","type":"session_meta","payload":{"id":"sess-cdx","timestamp":"`+start.Format(time.RFC3339Nano)+`","cwd":"`+cwd+`"}}`)

	id, ok := observeCodexSessionFile(cwd, start)
	if !ok || id != "sess-cdx" {
		t.Fatalf("got %q ok=%v", id, ok)
	}
	// A rollout for a different cwd is not a candidate.
	if _, ok := observeCodexSessionFile("/other/dir", start); ok {
		t.Fatal("mismatched cwd must not resolve")
	}
	// Rollouts outside the day folders around start are not scanned, even
	// when their meta claims an in-window timestamp.
	writeJSONL(t, filepath.Join(home, ".codex", "sessions", "2020", "01", "01",
		"rollout-2020-01-01T00-00-00-aaaa-bbbb-cccc-dddd-ffff.jsonl"),
		`{"type":"session_meta","payload":{"id":"sess-old","timestamp":"`+start.Format(time.RFC3339Nano)+`","cwd":"`+cwd+`"}}`)
	id2, ok := observeCodexSessionFile(cwd, start)
	if !ok || id2 != "sess-cdx" {
		t.Fatalf("out-of-day-folder rollout must not count; got %q ok=%v", id2, ok)
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
	if _, ok := observeCodexSessionFile(cwd, start); ok {
		t.Fatal("rollouts without a matching payload.cwd must not be candidates")
	}

	// payload.session_id is the fallback when payload.id is absent.
	rollout("aaaa-0000-0000-0000-000000000004", `"session_id":"sess-fb","timestamp":"`+ts+`","cwd":"`+cwd+`"`)
	id, ok := observeCodexSessionFile(cwd, start)
	if !ok || id != "sess-fb" {
		t.Fatalf("session_id fallback: got %q ok=%v", id, ok)
	}
}

// --- unit: ledger ----------------------------------------------------------

func TestPaneSessionLedger(t *testing.T) {
	teststate.Lock(t)
	base := t.TempDir()
	teststate.Setenv(t, "GTX_STATE_DIR", base)
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

// Closing a pane removes its ledger row (ownership cleanup boundary).
func TestLedgerDeleteOnPaneClose(t *testing.T) {
	teststate.Lock(t)
	base, err := os.MkdirTemp("/tmp", "gtxtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "GTX_STATE_DIR", base)
	teststate.Setenv(t, "SHELL", "/bin/sh")

	dir, done := startServerAt(t, "leddel")
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()

	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "right", Focus: true}, nil)
	ids := paneIDs(t, dir)
	reportAgent(t, dir, ids[1], "claude", "sess-x")

	var ok api.OkResult
	apiOK(t, dir, api.MethodPaneClose, api.PaneTarget{PaneID: ids[1]}, &ok)

	store, err := dispatch.OpenReadOnly(dispatch.DBPath("leddel"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, _ := store.PaneSessions("leddel")
	for _, r := range rows {
		if r.PaneID == 2 {
			t.Fatalf("ledger row for closed pane survives: %+v", r)
		}
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

func waitPanes(t *testing.T, dir string, want []string) []string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var ids []string
	for time.Now().Before(deadline) {
		ids = paneIDs(t, dir)
		if len(ids) == len(want) {
			same := true
			for i := range ids {
				if ids[i] != want[i] {
					same = false
				}
			}
			if same {
				return ids
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return ids
}

// G3 restorable pane retention: a pane carrying an agent label and a safe
// recorded session id keeps its pane number, record, and ledger row when
// its process exits — it comes back as a shell instead of closing.
func TestRestorablePaneRetainedOnExit(t *testing.T) {
	teststate.Lock(t)
	base, err := os.MkdirTemp("/tmp", "gtxtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "GTX_STATE_DIR", base)
	teststate.Setenv(t, "SHELL", "/bin/sh")

	dir, done := startServerAt(t, "retain")
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()

	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "right", Focus: true}, nil)
	ids := paneIDs(t, dir)
	if len(ids) != 2 {
		t.Fatalf("panes = %v", ids)
	}
	reportAgent(t, dir, ids[1], "claude", "sess-keep")
	paneExit(t, dir, ids[1])

	got := waitPanes(t, dir, ids)
	if len(got) != 2 {
		t.Fatalf("restorable pane was closed; panes = %v", got)
	}

	// The pane's record and ledger row must survive.
	store, err := dispatch.OpenReadOnly(dispatch.DBPath("retain"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, _ := store.PaneSessions("retain")
	found := false
	for _, r := range rows {
		if r.SessionID == "sess-keep" && r.Agent == "claude" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ledger lost the retained pane's row: %v", rows)
	}

	// The snapshot keeps agent/session id after detection cleared.
	f := waitForSessionFile(t, dir)
	kept := false
	f.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) {
		if p.SessionID == "sess-keep" && p.Agent == "claude" {
			kept = true
		}
	})
	if !kept {
		t.Fatal("session.json lost the retained pane's agent/session id")
	}
}

// G3 inactive-shell cleanup: a pane whose only trace is a past ledger
// row/record — no effective agent label — closes normally on exit, and
// its ledger row goes with it.
func TestInactiveShellPaneClosesOnExit(t *testing.T) {
	teststate.Lock(t)
	base, err := os.MkdirTemp("/tmp", "gtxtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "GTX_STATE_DIR", base)
	teststate.Setenv(t, "SHELL", "/bin/sh")

	dir, done := startServerAt(t, "cleanup")
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()

	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "right", Focus: true}, nil)
	ids := paneIDs(t, dir)
	if len(ids) != 2 {
		t.Fatalf("panes = %v", ids)
	}
	// Record + ledger exist, then the report is cleared — the pane is a
	// plain shell again and must close normally.
	reportAgent(t, dir, ids[1], "codex", "sess-old")
	reportAgent(t, dir, ids[1], "", "")
	paneExit(t, dir, ids[1])

	got := waitPanes(t, dir, ids[:1])
	if len(got) != 1 || got[0] != ids[0] {
		t.Fatalf("shell pane was retained; panes = %v", got)
	}

	store, err := dispatch.OpenReadOnly(dispatch.DBPath("cleanup"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, _ := store.PaneSessions("cleanup")
	for _, r := range rows {
		if r.PaneID == 2 {
			t.Fatalf("ledger row for closed pane survives: %+v", r)
		}
	}
}

// SPEC G3 fossil expiry + stale authority clearing, end to end: a pane
// labelled claude whose screen shows a frozen spinner drops to idle
// after ~15s, and a reported "working" authority over an explicitly
// idle Claude screen is cleared after ~15s.
