package e2e

import (
	"encoding/json"
	"fmt"
	"os"

	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	sqlite3 "github.com/ncruces/go-sqlite3"
)

func TestPaneRunStoresCallerGeneration(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	s := start(t)
	s.waitFor("prompt", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	if raw, err := s.cli("pane", "report-agent", "p1", "--source", "test", "--agent", "codex", "--state", "blocked"); err != nil {
		t.Fatal(raw, err)
	}
	fifo := filepath.Join(t.TempDir(), "release")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	release, err := os.OpenFile(fifo, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release.Close()
	defer release.WriteString("go\n")
	raw, err := s.cli("run", "--pane", "--caller", "p1", "--label", "generation", "--", "/bin/sh", "-c", fmt.Sprintf("read x < %q", fifo))
	if err != nil {
		t.Fatal(raw, err)
	}
	var started struct {
		Job string `json:"job"`
	}
	if err := json.Unmarshal([]byte(raw), &started); err != nil || started.Job == "" {
		t.Fatal(raw, err)
	}
	db, err := sqlite3.OpenFlags(filepath.Join(s.stateDir, "sessions", s.name, "ikesu.db"), sqlite3.OPEN_READONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmt, _, err := db.Prepare(`SELECT COALESCE(pane_generation,'') FROM dispatches WHERE kind='command' AND external_id=?`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := stmt.BindText(1, started.Job); err != nil {
		t.Fatal(err)
	}
	if !stmt.Step() {
		t.Fatalf("job row missing: %v", stmt.Err())
	}
	gen := stmt.ColumnText(0)
	if gen == "" {
		t.Fatal("run --pane did not save the caller generation")
	}
	stmt.Close()
	if _, err := release.WriteString("go\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		check, _, err := db.Prepare(`SELECT j.status, COALESCE(m.pane_generation,'') FROM dispatches j JOIN dispatches m ON m.kind='message' AND m.body LIKE '%' || j.external_id || '%' WHERE j.kind='command' AND j.external_id=?`)
		if err != nil {
			t.Fatal(err)
		}
		if err := check.BindText(1, started.Job); err != nil {
			t.Fatal(err)
		}
		if check.Step() {
			status, noticeGen := check.ColumnText(0), check.ColumnText(1)
			check.Close()
			if status != "exited" || noticeGen != gen {
				t.Fatalf("completion status=%s notice generation=%q caller generation=%q", status, noticeGen, gen)
			}
			return
		}
		check.Close()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("completion notice missing")
}
