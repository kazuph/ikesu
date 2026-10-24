package e2e

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestShortCommandKeepsActualExitAndNotice(t *testing.T) {
	j := startFaultJob(t)
	var result api.RunStartedResult
	j.s.rpc(api.MethodRunStart, api.RunStartParams{CallerPane: "p1", Label: "short-command", Completion: "summary", Argv: []string{"/usr/bin/true"}}, &result)
	j.s.waitFor("short command real zero and notice", 10*time.Second, func(string) bool {
		row, _ := j.store.CommandRow(result.Job)
		b, _ := os.ReadFile(j.sink)
		return row != nil && row.Status == "exited" && row.ExitCode != nil && *row.ExitCode == 0 && strings.Count(string(b), "job="+result.Job+" ") == 1
	})
}

func TestExpiredDiagnosticLockDoesNotBlockCollectionOrStop(t *testing.T) {
	j := startFaultJob(t)
	log := filepath.Join(filepath.Dir(j.job.LogPath), "expired-review.log")
	for _, suffix := range []string{"", ".monitor-stderr.log", ".monitor-exit.json"} {
		if err := os.WriteFile(log+suffix, []byte("expired"), 0600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-2 * dispatch.JobRetention)
		if err := os.Chtimes(log+suffix, old, old); err != nil {
			t.Fatal(err)
		}
	}
	writer, err := dispatch.Open(j.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.InsertCommand("expired-review", "expired-review", "true", "/", "p1", "owner", "none", log); err != nil {
		t.Fatal(err)
	}
	if err = writer.MarkCommandRunning("expired-review", int64(os.Getpid())); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.MarkCommandFinished("expired-review", nil); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	faultSQL(t, j.dbPath, fmt.Sprintf("UPDATE dispatches SET finished_at='%s' WHERE external_id='expired-review'", time.Now().Add(-2*dispatch.JobRetention).UTC().Format("2006-01-02T15:04:05.000Z")))
	held, err := os.Open(log)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err = syscall.Flock(int(held.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			syscall.Flock(int(held.Fd()), syscall.LOCK_UN)
		}
	}()
	handoff := make(chan error, 1)
	go func() { _, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); handoff <- err }()
	select {
	case err = <-handoff:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		syscall.Flock(int(held.Fd()), syscall.LOCK_UN)
		locked = false
		<-handoff
		t.Fatal("expired diagnostic lock blocked handoff")
	}
	var normal api.RunStartedResult
	j.s.rpc(api.MethodRunStart, api.RunStartParams{CallerPane: "p1", Label: "lock-control", Completion: "summary", Argv: []string{"/usr/bin/true"}}, &normal)
	data, err := os.ReadFile(j.job.LogPath + ".runner")
	if err != nil {
		t.Fatal(err)
	}
	var m lossManifest
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	signalLossProcess(t, m.MonitorPID, syscall.SIGKILL, binPath)
	if _, err = j.release.WriteString("go\n"); err != nil {
		t.Fatal(err)
	}
	j.s.waitFor("normal and lost jobs collected despite expired locked log", 10*time.Second, func(string) bool {
		lost, _ := j.store.CommandRow(j.job.ID)
		ok, _ := j.store.CommandRow(normal.Job)
		b, _ := os.ReadFile(j.sink)
		return lost != nil && lost.Status == "exited" && lost.ExitCode == nil && j.notices() == 1 && ok != nil && ok.Status == "exited" && ok.ExitCode != nil && *ok.ExitCode == 0 && strings.Count(string(b), "job="+normal.Job+" ") == 1
	})
	for _, suffix := range []string{"", ".monitor-stderr.log", ".monitor-exit.json"} {
		if _, err = os.Stat(log + suffix); err != nil {
			t.Fatal("busy family removed", suffix, err)
		}
	}
	j.s.stopServer()
	syscall.Flock(int(held.Fd()), syscall.LOCK_UN)
	locked = false
	writer, err = dispatch.Open(j.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err = writer.CleanupJobs(time.Now(), filepath.Dir(log), func(pid int) bool { return syscall.Kill(pid, 0) == nil }); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", ".monitor-stderr.log", ".monitor-exit.json"} {
		if _, err = os.Stat(log + suffix); !os.IsNotExist(err) {
			t.Fatal("deferred family retained", suffix, err)
		}
	}
}
