package dispatch

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestG9JobRetentionRepliesOrphanLogsAndKindIndex(t *testing.T) {
	s := testStore(t)
	logDir := t.TempDir()
	now := time.Now().UTC()
	old := now.Add(-25 * time.Hour).Format(tsLayout)
	recent := now.Add(-time.Hour).Format(tsLayout)
	runner := exec.Command("/bin/sleep", "60")
	runner.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := runner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { runner.Process.Kill(); runner.Wait() }()
	for _, name := range []string{"old-finished", "recent-finished", "old-dead", "old-live"} {
		log := filepath.Join(logDir, name+".log")
		if err := os.WriteFile(log, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		if err := s.InsertCommand(name, name, "true", logDir, "p1", "owner", "none", log); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.exec(`UPDATE dispatches SET created_at=?, status='running'`, old); err != nil {
		t.Fatal(err)
	}
	if err := s.exec(`UPDATE dispatches SET status='exited', finished_at=? WHERE external_id='old-finished'`, old); err != nil {
		t.Fatal(err)
	}
	if err := s.exec(`UPDATE dispatches SET status='exited', finished_at=? WHERE external_id='recent-finished'`, recent); err != nil {
		t.Fatal(err)
	}
	if err := s.exec(`UPDATE dispatches SET runner_pid=? WHERE external_id='old-live'`, runner.Process.Pid); err != nil {
		t.Fatal(err)
	}
	st, err := s.queryLocked(`SELECT id FROM dispatches WHERE external_id='old-finished'`)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Step() {
		t.Fatal("job missing")
	}
	id := st.ColumnInt64(0)
	st.Close()
	message, err := s.InsertMessage(JobsRoom, "", "owner", "owner", "preserve completion body", &id, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old-orphan", "recent-orphan"} {
		path := filepath.Join(logDir, name+".log")
		if err := os.WriteFile(path, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		if name == "old-orphan" {
			if err := os.Chtimes(path, now.Add(-25*time.Hour), now.Add(-25*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}
	alive := func(pid int) bool { err := syscall.Kill(-pid, 0); return err == nil || err == syscall.EPERM }
	if err := s.CleanupJobs(now, logDir, alive); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.CommandRows(0)
	if err != nil || len(jobs) != 2 {
		t.Fatal(jobs, err)
	}
	for _, name := range []string{"old-finished", "old-dead", "old-orphan"} {
		if _, err := os.Stat(filepath.Join(logDir, name+".log")); !os.IsNotExist(err) {
			t.Fatal("expired log retained", name, err)
		}
	}
	for _, name := range []string{"recent-finished", "old-live", "recent-orphan"} {
		if _, err := os.Stat(filepath.Join(logDir, name+".log")); err != nil {
			t.Fatal("retained log lost", name, err)
		}
	}
	st, err = s.queryLocked(`SELECT body, reply_to FROM dispatches WHERE id=?`, message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Step() || st.ColumnText(0) != message.Body || st.ColumnType(1).String() != "NULL" {
		t.Fatal("completion message lost/unlink failed")
	}
	st.Close()
	st, err = s.queryLocked(`EXPLAIN QUERY PLAN SELECT id FROM dispatches WHERE kind='command' ORDER BY id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for st.Step() {
		plan.WriteString(st.ColumnText(3))
	}
	st.Close()
	if !strings.Contains(plan.String(), "idx_dispatch_kind_id") || strings.Contains(plan.String(), "TEMP B-TREE") {
		t.Fatal(plan.String())
	}
}

func TestG9CleanupWaitsForOtherProcessWriter(t *testing.T) {
	if path := os.Getenv("GO_TEST_SQLITE_WRITER_PATH"); path != "" {
		writer, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		if err := writer.db.Exec("BEGIN IMMEDIATE"); err != nil {
			t.Fatal(err)
		}
		fmt.Println("LOCKED")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || line != "COMMIT\n" {
			t.Fatal(line, err)
		}
		if err := writer.db.Exec("COMMIT"); err != nil {
			t.Fatal(err)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "gtx.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	writer := exec.Command(executable, "-test.run=^TestG9CleanupWaitsForOtherProcessWriter$")
	writer.Env = append(os.Environ(), "GO_TEST_SQLITE_WRITER_PATH="+path)
	writer.Stderr = os.Stderr
	in, err := writer.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := writer.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { in.Close(); writer.Process.Kill(); writer.Wait() }()
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "LOCKED\n" {
		t.Fatal(line, err)
	}
	result := make(chan error, 1)
	go func() {
		result <- s.CleanupJobs(time.Now(), t.TempDir(), func(pid int) bool { err := syscall.Kill(pid, 0); return err == nil || err == syscall.EPERM })
	}()
	select {
	case err := <-result:
		t.Fatal("cleanup did not wait for transaction", err)
	case <-time.After(100 * time.Millisecond):
	}
	fmt.Fprintln(in, "COMMIT")
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not finish after writer commit")
	}
}
