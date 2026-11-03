package e2e

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/dispatch"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise real CLI processes while the server updates its pane/session ledger.
func TestCLIStartupDuringServerWrites(t *testing.T) {
	s := start(t)
	s.waitFor("prompt", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	stop := make(chan struct{})
	started := make(chan struct{})
	var writes atomic.Int64
	done := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			if _, err := s.cli("pane", "report-agent", "p1", "--source", "test", "--agent", "codex", "--state", "blocked", "--session-id", fmt.Sprintf("sqlite-writer-%d", writes.Load()+1)); err != nil {
				done <- err
				return
			}
			if writes.Add(1) == 1 {
				close(started)
			}
		}
	}()
	defer func() {
		close(stop)
		if err := <-done; err != nil {
			t.Error("server writer:", err)
			if writes.Add(1) == 1 {
				close(started)
			}
		}
		st, err := dispatch.OpenReadOnly(filepath.Join(s.stateDir, "sessions", s.name, "ikesu.db"))
		if err != nil {
			t.Error(err)
			return
		}
		defer st.Close()
		rows, err := st.PaneSessions(s.name)
		found := false
		for _, row := range rows {
			if row.PaneID == 1 && row.SessionID == fmt.Sprintf("sqlite-writer-%d", writes.Load()) {
				found = true
			}
		}
		if err != nil || !found {
			t.Errorf("server writes were not persisted: rows=%+v err=%v", rows, err)
		}
		t.Logf("CLI startups=20 server writes=%d", writes.Load())
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("server writer did not start")
	}
	for attempt := 0; attempt < 20; attempt++ {
		raw, err := s.cli("run", "--pane", "--caller", "p1", "--label", "concurrent-start", "--", "/bin/sh", "-c", "exit 7")
		if err != nil {
			t.Fatalf("CLI startup attempt %d: %s %v", attempt, raw, err)
		}
	}
}
