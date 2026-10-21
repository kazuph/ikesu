package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kazuph/goterrace/internal/dispatch"
)

func TestJobResultIdentityAndWriteErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "job.log")
	pid, started := int64(os.Getpid()), int64(1234)
	job := dispatch.Job{ID: "j0001", LogPath: path, RunnerPID: &pid, StartedMs: &started}
	manifest := jobRunnerRecord{JobID: job.ID, PID: int(pid), StartedMs: started, Token: "private-token", Identity: "original-process"}
	manifest.MonitorPID = os.Getpid()
	manifest.MonitorIdentity = "monitor-process"
	if err := writeJobRecord(path+".runner", manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := managedJobRecord(job); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*jobRunnerRecord)
	}{
		{"token", func(r *jobRunnerRecord) { r.Token = "another-job" }},
		{"pid", func(r *jobRunnerRecord) { r.PID++ }},
		{"start", func(r *jobRunnerRecord) { r.StartedMs++ }},
		{"process", func(r *jobRunnerRecord) { r.Identity = "reused-pid" }},
		{"time", func(r *jobRunnerRecord) { r.FinishedMs = started - 1 }},
		{"log-write", func(r *jobRunnerRecord) { r.Error = "log write failed" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := manifest
			result.ExitCode = 7
			result.FinishedMs = started + 1
			tc.change(&result)
			if err := writeJobRecord(path+".result", result); err != nil {
				t.Fatal(err)
			}
			if _, err := completedJobRecord(job, manifest); err == nil {
				t.Fatal("invalid completion accepted")
			}
		})
	}
	if err := os.WriteFile(path+".result", []byte(`{"JobID":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := completedJobRecord(job, manifest); err == nil {
		t.Fatal("partial JSON accepted")
	}
	if err := os.Mkdir(path+".blocked", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path+".blocked", "keep"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeJobRecord(path+".blocked", manifest); err == nil {
		t.Fatal("rename failure accepted")
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".job-result-*"))
	if err != nil || len(files) != 0 {
		t.Fatal("temporary result leaked", files, err)
	}
}

func TestRunnerProcessIdentityUsesRealStart(t *testing.T) {
	first, err := jobProcessIdentity(os.Getpid())
	if err != nil || first == "" {
		t.Fatal(first, err)
	}
	second, err := jobProcessIdentity(os.Getpid())
	if err != nil || first != second {
		t.Fatal("unstable process identity", first, second, err)
	}
}
