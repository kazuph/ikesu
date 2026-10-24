package e2e

import (
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	sqlite3 "github.com/ncruces/go-sqlite3"
)

func TestResultLossAndCancelArbitrateInRealSQLite(t *testing.T) {
	for _, first := range []string{"recovery", "cancel"} {
		t.Run(first, func(t *testing.T) {
			j := startFaultJob(t)
			data, err := os.ReadFile(j.job.LogPath + ".runner")
			if err != nil {
				t.Fatal(err)
			}
			var m lossManifest
			if err = json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			signalLossProcess(t, m.MonitorPID, syscall.SIGKILL, binPath)
			blocker, err := sqlite3.Open(j.dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Close()
			if err = blocker.Exec("BEGIN IMMEDIATE"); err != nil {
				t.Fatal(err)
			}
			defer blocker.Exec("ROLLBACK")
			cancelDone := make(chan struct{}, 1)
			cancel := func() { _, _ = j.s.cli("run", "cancel", j.job.ID); cancelDone <- struct{}{} }
			if first == "cancel" {
				go cancel()
				j.s.waitFor("cancel prepared before its blocked DB write", 5*time.Second, func(string) bool { _, err := os.Stat(j.job.LogPath + ".cancel"); return err == nil })
			}
			if _, err = j.release.WriteString("go\n"); err != nil {
				t.Fatal(err)
			}
			if first == "recovery" {
				j.s.waitFor("recovery prepared before its blocked DB write", 5*time.Second, func(string) bool {
					_, err := os.Stat(j.job.LogPath + ".completion-unavailable.json")
					return err == nil
				})
				go cancel()
			}
			if err = blocker.Exec("COMMIT"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-cancelDone:
			case <-time.After(10 * time.Second):
				t.Fatal("cancel did not return")
			}
			want := "exited"
			if first == "cancel" {
				want = "cancelled"
			}
			j.s.waitFor("one terminal state and real PTY notice after contention", 10*time.Second, func(string) bool {
				r, _ := j.store.CommandRow(j.job.ID)
				return r != nil && r.Status == want && r.ExitCode == nil && j.notices() == 1
			})
			data, _ = os.ReadFile(j.sink)
			if first == "recovery" && !strings.Contains(string(data), "exit=unknown") {
				t.Fatal(string(data))
			}
			if out, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
				t.Fatal(out, err)
			}
			if j.notices() != 1 {
				t.Fatal("contention duplicated notice", j.notices())
			}
		})
	}
}
