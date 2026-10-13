package e2e

import (
	"encoding/json"
	"github.com/kazuph/goterrace/internal/dispatch"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPaneRunPersistsExactCallerAndFailureCompletion(t *testing.T) {
	s := start(t)
	s.waitFor("prompt", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	if raw, err := s.cli("pane", "report-agent", "p1", "--source", "test", "--agent", "codex", "--state", "blocked"); err != nil {
		t.Fatal(raw, err)
	}
	raw, err := s.cli("run", "--pane", "--caller", "p_1", "--label", "exact-pane", "--", "/bin/sh", "-c", "exit 7")
	if err != nil {
		t.Fatal(raw, err)
	}
	var spawned struct {
		Job  string `json:"job"`
		Pane string `json:"pane"`
	}
	if err := json.Unmarshal([]byte(raw), &spawned); err != nil || spawned.Job == "" || spawned.Pane == "" {
		t.Fatal(raw, err)
	}
	store, err := dispatch.OpenReadOnly(filepath.Join(s.stateDir, "sessions", s.name, "gtx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		row, err := store.CommandRow(spawned.Job)
		if err != nil || row == nil {
			t.Fatal(row, err)
		}
		if row.CallerPane != "p1" {
			t.Fatal("caller was not frozen at creation", row)
		}
		pending, err := store.PendingJobMessages()
		if err != nil {
			t.Fatal(err)
		}
		if row.Status == "exited" && len(pending) > 0 {
			if row.ExitCode == nil || *row.ExitCode != 7 || len(pending) != 1 || pending[0].ToAgent != "p1" || !strings.Contains(pending[0].Body, "exit=7") {
				t.Fatal(row, pending)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pane job failure completion missing", row, pending)
		}
		s.pump(time.Now().Add(10 * time.Millisecond))
	}
}
