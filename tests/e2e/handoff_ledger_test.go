package e2e

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	sqlite3 "github.com/ncruces/go-sqlite3"
)

func TestLiveHandoffWaitsForPendingLedger(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ledger.db")
	s := startEnv(t, map[string]string{"IKESU_DB_PATH": dbPath})
	s.waitFor("shell", 15*time.Second, func(screen string) bool { return strings.Contains(screen, "❯") })
	store, err := dispatch.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	writer, err := sqlite3.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_ = writer.Exec("ROLLBACK")
		}
	}()
	id := "pending-handoff-session"
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Agent: "codex", State: "idle", Source: "test", SessionID: &id}, nil)
	type result struct {
		output string
		err    error
	}
	done := make(chan result, 1)
	go func() { out, err := s.cli("server", "live-handoff"); done <- result{out, err} }()
	var early *result
	select {
	case r := <-done:
		early = &r
	case <-time.After(250 * time.Millisecond):
	}
	if early == nil {
		conn, err := net.DialTimeout("unix", api.APISocketPath(filepath.Join(s.stateDir, s.name)), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := api.WriteRequest(conn, api.Request{ID: "during-handoff", Method: api.MethodPaneGet, Params: api.MarshalParams(map[string]any{"pane_id": "p1"})}); err != nil {
			t.Fatal(err)
		}
		var response api.Response
		if err := json.NewDecoder(conn).Decode(&response); err != nil {
			t.Fatalf("pane.get while ledger is locked: %v", err)
		}
		if response.Err != nil {
			t.Fatalf("pane.get while ledger is locked: %+v", response.Err)
		}
	}
	if err := writer.Exec("COMMIT"); err != nil {
		t.Fatal(err)
	}
	locked = false
	if early != nil {
		t.Fatalf("handoff completed before accepted ledger write: %s %v", early.output, early.err)
	}
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("handoff: %s %v", r.output, r.err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("handoff did not finish after ledger was unlocked")
	}
	store, err = dispatch.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.PaneSessions(s.name)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SessionID != id {
		t.Fatalf("lost ledger row: %+v", rows)
	}
}
