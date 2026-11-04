package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/dispatch"
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

func TestPaneRunRollsBackCompletionWhenNoticeInsertFails(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	s := start(t)
	s.waitFor("prompt", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	fifo := filepath.Join(t.TempDir(), "release")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	release, err := os.OpenFile(fifo, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release.Close()
	raw, err := s.cli("run", "--pane", "--caller", "p1", "--label", "atomic", "--", "/bin/sh", "-c", fmt.Sprintf("read x < %q", fifo))
	if err != nil {
		t.Fatal(raw, err)
	}
	var started struct {
		Job  string `json:"job"`
		Pane string `json:"pane"`
	}
	if err := json.Unmarshal([]byte(raw), &started); err != nil || started.Job == "" || started.Pane == "" {
		t.Fatal(raw, err)
	}
	db, err := sqlite3.Open(filepath.Join(s.stateDir, "sessions", s.name, "ikesu.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Exec(`CREATE TRIGGER reject_pane_notice BEFORE INSERT ON dispatches WHEN NEW.kind='message' BEGIN SELECT RAISE(ABORT,'notice failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := release.WriteString("go\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	closed := false
	for time.Now().Before(deadline) {
		_, err := s.cli("pane", "get", started.Pane)
		if err != nil {
			closed = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !closed {
		t.Fatal("job pane did not close")
	}
	stmt, _, err := db.Prepare(`SELECT status FROM dispatches WHERE kind='command' AND external_id=?`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := stmt.BindText(1, started.Job); err != nil {
		t.Fatal(err)
	}
	if !stmt.Step() {
		t.Fatal("job row missing", stmt.Err())
	}
	if status := stmt.ColumnText(0); status != "running" {
		t.Fatalf("completion was not rolled back after notice insert failed: status=%s", status)
	}
}

func TestPaneRunCompletionWhileServerStopped(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	stateDir := t.TempDir()
	dbPath := filepath.Join(stateDir, "sessions", "offline", "ikesu.db")
	store, err := dispatch.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertPaneCommand("job-offline", "offline", "true", "", "p1", "p1", "summary", dispatch.JobLogPath("job-offline", "offline", "offline"), "original-generation"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	cmd := exec.Command(binPath, "--session", "offline", "__pane-run", "--target", "p2", "--job-id", "job-offline", "--run-label", "offline", "--close-on-exit", "--", "true")
	cmd.Env = append(os.Environ(), "IKESU_STATE_DIR="+stateDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("wrapper failed: %v: %s", err, output)
	}
	db, err := sqlite3.OpenFlags(dbPath, sqlite3.OPEN_READONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmt, _, err := db.Prepare(`SELECT j.status, COALESCE(m.pane_generation,'') FROM dispatches j JOIN dispatches m ON m.kind='message' AND m.body LIKE '%' || j.external_id || '%' WHERE j.external_id='job-offline'`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if !stmt.Step() {
		t.Fatalf("offline completion notice missing: %v", stmt.Err())
	}
	if status, generation := stmt.ColumnText(0), stmt.ColumnText(1); status != "exited" || generation != "original-generation" {
		t.Fatalf("offline completion status=%s generation=%q", status, generation)
	}
}
