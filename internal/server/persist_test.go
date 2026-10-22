package server

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/session"
)

// paneIDs returns the sorted pane ids reported by pane.list.
func paneIDs(t *testing.T, dir string) []string {
	t.Helper()
	var list api.PaneListResult
	apiOK(t, dir, api.MethodPaneList, api.PaneListParams{}, &list)
	var ids []string
	for _, p := range list.Panes {
		ids = append(ids, p.PaneID)
	}
	sort.Strings(ids)
	return ids
}

// pinTestConfig points IKESU_CONFIG at a test config that disables
// resume_agents_on_restore so the startup auto-restore can never launch
// a real agent binary from a test's reported session record. Tests that
// set their own IKESU_CONFIG before calling keep it.
func pinTestConfig(t *testing.T) {
	t.Helper()
	if os.Getenv("IKESU_CONFIG") != "" {
		return
	}
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "IKESU_CONFIG", cfg)
}

// startServerAt boots a server for sess under an existing IKESU_STATE_DIR
// base — unlike startTestServer it does not make a fresh base, so a test
// can run two servers over the same state dir back to back.
func startServerAt(t *testing.T, sess string) (dir string, done chan int) {
	t.Helper()
	pinTestConfig(t)
	dir = api.SessionDir(sess)
	done = make(chan int, 1)
	go func() {
		code, err := Run(sess, "test-version")
		if err != nil {
			t.Errorf("server.Run: %v", err)
		}
		done <- code
	}()
	waitAPISocket(t, dir)
	return dir, done
}

// waitForSessionFile polls until session.json exists and parses.
func waitForSessionFile(t *testing.T, dir string) *session.File {
	t.Helper()
	path := session.Path(dir)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if f, err := session.Load(path); err == nil && f != nil {
			return f
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("session.json never appeared at %s", path)
	return nil
}

// G3/G5: a split and a rename are saved to session.json, and a fresh
// server process restores the same pane numbers, layout, and labels.
func TestSessionSaveAndReload(t *testing.T) {
	teststate.Lock(t)
	base, err := os.MkdirTemp("/tmp", "ikesutest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "IKESU_STATE_DIR", base)
	teststate.Setenv(t, "SHELL", "/bin/sh")

	dir, done := startServerAt(t, "persist")

	ids := paneIDs(t, dir)
	if len(ids) != 1 {
		t.Fatalf("initial panes = %v, want 1", ids)
	}
	firstID := ids[0]

	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "right", Focus: true}, nil)
	label := "main"
	apiOK(t, dir, api.MethodPaneRename, api.PaneRenameParams{PaneID: firstID, Label: &label}, nil)

	f := waitForSessionFile(t, dir)
	var paneRecs []session.Pane
	f.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) {
		paneRecs = append(paneRecs, *p)
	})
	if len(paneRecs) != 2 {
		t.Fatalf("session.json has %d panes, want 2", len(paneRecs))
	}

	// Stop, then boot a fresh server over the same state dir — exactly
	// what a restart's successor does.
	stopServer(t, dir, done)

	dir2, done2 := startServerAt(t, "persist")
	defer stopServer(t, dir2, done2)
	if dir2 != dir {
		t.Fatalf("session dir changed: %s vs %s", dir2, dir)
	}
	got := paneIDs(t, dir)
	if len(got) != 2 || got[0] != firstID || got[1] != "p2" {
		t.Fatalf("restored pane ids %v, want [%s p2]", got, firstID)
	}

	// The restored label survives too.
	var info api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneGet, api.PaneQueryParams{PaneID: firstID}, &info)
	if info.Pane.Label == nil || *info.Pane.Label != "main" {
		t.Fatalf("pane label not restored: %+v", info.Pane.Label)
	}

	// New panes after restore must not reuse a restored number (G5).
	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "down", Focus: true}, nil)
	got = paneIDs(t, dir)
	if len(got) != 3 || got[2] != "p3" {
		t.Fatalf("panes after new split = %v, want [%s p2 p3]", got, firstID)
	}
}

// G3: server.restart saves, then stops. The unit-test successor is the
// test binary itself, so it cannot serve — this verifies the ok response,
// the exit, and that session.json is intact. The real successor and
// client reconnect paths are covered by the e2e binary test.
func TestServerRestartRespondsAndSaves(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	waitAPISocket(t, dir)
	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "right"}, nil)
	waitForSessionFile(t, dir)

	resp := apiReq(t, dir, api.MethodServerRestart, api.EmptyParams{})
	if resp.Err != nil {
		t.Fatalf("server.restart: %+v", resp.Err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit after server.restart")
	}
	f, err := session.Load(session.Path(dir))
	if err != nil || f == nil {
		t.Fatalf("session.json missing after restart: %v", err)
	}
}

// A corrupt session.json must not crash startup; the server fails closed
// to a fresh single-pane state.
func TestCorruptSessionStartsFresh(t *testing.T) {
	teststate.Lock(t)
	base, err := os.MkdirTemp("/tmp", "ikesutest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "IKESU_STATE_DIR", base)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	dir := api.SessionDir("corrupt")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir2, done := startServerAt(t, "corrupt")
	defer stopServer(t, dir2, done)

	var list api.PaneListResult
	apiOK(t, dir, api.MethodPaneList, api.PaneListParams{}, &list)
	if len(list.Panes) != 1 {
		t.Fatalf("fresh panes = %d, want 1", len(list.Panes))
	}
}

// A client attached to the client socket receives {"type":"restart"}
// before the connection closes on server.restart (G3 restart).
func TestRestartNotifiesClient(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	waitAPISocket(t, dir)
	c := dialClient(t, dir)
	c.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })

	resp := apiReq(t, dir, api.MethodServerRestart, api.EmptyParams{})
	if resp.Err != nil {
		t.Fatalf("restart: %+v", resp.Err)
	}
	msgs := c.readUntil(t, 5*time.Second, func(m clientMsg) bool {
		return m.Type == "restart" || m.Type == "detached" || m.Type == "exit"
	})
	found := false
	for _, m := range msgs {
		if m.Type == "restart" {
			found = true
		}
	}
	if !found {
		var types []string
		for _, m := range msgs {
			types = append(types, m.Type)
		}
		t.Fatalf("client never got restart msg; saw %v", types)
	}
	waitServerExit(t, done)
}
