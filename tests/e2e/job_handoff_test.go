package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
)

func TestJobsAcrossLiveHandoff(t *testing.T) {
	for _, tc := range []struct {
		name     string
		handoffs int
		cancel   bool
	}{
		{"once", 1, false}, {"repeated", 3, false}, {"cancel", 1, true}, {"cancel-term", 1, true}, {"rollback", 1, false}, {"full", 1, false}, {"boundary", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := start(t)
			s.waitFor("shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
			s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "job-handoff", Agent: "codex", State: "idle"}, nil)
			fifo := filepath.Join(t.TempDir(), "release")
			if err := syscall.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			release, err := os.OpenFile(fifo, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer release.Close()
			var started api.RunStartedResult
			script := fmt.Sprintf("trap '' TERM; printf 'READY\\n'; read value < %q; printf 'LAST_OUT\\n'; printf 'LAST_ERR\\n' >&2; exit 7", fifo)
			if tc.name == "cancel-term" {
				script = strings.TrimPrefix(script, "trap '' TERM; ")
			}
			completion := "summary"
			if tc.name == "full" {
				completion = "full"
			}
			s.rpc(api.MethodRunStart, api.RunStartParams{CallerPane: "p1", Label: "handoff", Completion: completion, Argv: []string{"/bin/sh", "-c", script}}, &started)
			store, err := dispatch.OpenReadOnly(filepath.Join(s.stateDir, "sessions", s.name, "ikesu.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			initial, err := store.CommandRow(started.Job)
			if err != nil || initial == nil || initial.RunnerPID == nil {
				t.Fatal(initial, err)
			}
			pid := int(*initial.RunnerPID)
			t.Cleanup(func() {
				if syscall.Kill(pid, 0) == nil {
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}
			})
			s.waitFor("job ready", 10*time.Second, func(string) bool {
				data, _ := os.ReadFile(initial.LogPath)
				return strings.Contains(string(data), "[stdout] READY\n")
			})
			if tc.name == "rollback" {
				if out, err := s.cli("server", "live-handoff", "--import-exe", binPath, "--expected-version", "invalid-version"); err == nil || !strings.Contains(out, "handoff_failed") {
					t.Fatal("rollback not exercised", out, err)
				}
			}
			for i := 0; i < tc.handoffs; i++ {
				if tc.name == "boundary" && i == 0 {
					if _, err := release.WriteString("go\n"); err != nil {
						t.Fatal(err)
					}
				}
				if out, err := s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
					t.Fatal(out, err)
				}
				row, err := store.CommandRow(started.Job)
				if err != nil || row == nil || row.RunnerPID == nil || *row.RunnerPID != int64(pid) || tc.name != "boundary" && syscall.Kill(pid, 0) != nil {
					t.Fatal("job identity lost", row, err)
				}
			}
			status, notice := "exited", "[ikesu run] exit=7 label=handoff job="+started.Job
			if tc.cancel {
				var cancelled api.RunCancelledResult
				s.rpc(api.MethodRunCancel, api.RunCancelParams{JobID: started.Job}, &cancelled)
				if cancelled.Status != "cancelling" {
					t.Fatal(cancelled)
				}
				if tc.name != "cancel-term" {
					if out, err := s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
						t.Fatal(out, err)
					}
				}
				status, notice = "cancelled", "[ikesu run] cancelled label=handoff job="+started.Job
			} else if tc.name != "boundary" {
				if _, err = release.WriteString("go\n"); err != nil {
					t.Fatal(err)
				}
			}
			s.waitFor("terminal job and notice", 15*time.Second, func(text string) bool {
				row, _ := store.CommandRow(started.Job)
				return row != nil && row.Status == status && row.FinishedMs != nil && strings.Contains(text, notice)
			})
			row, _ := store.CommandRow(started.Job)
			data, err := os.ReadFile(row.LogPath)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.cancel && (row.ExitCode == nil || *row.ExitCode != 7 || !strings.Contains(string(data), "[stdout] LAST_OUT\n") || !strings.Contains(string(data), "[stderr] LAST_ERR\n")) {
				t.Fatal(row, string(data))
			}
			if tc.cancel && syscall.Kill(pid, 0) == nil {
				t.Fatal("cancelled runner still alive")
			}
			if out, err := s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
				t.Fatal(out, err)
			}
			s.waitFor("client reattached", 5*time.Second, func(text string) bool { return strings.Contains(text, notice) })
			messages, err := store.History(dispatch.JobsRoom, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, m := range messages {
				if strings.Contains(m.Body, "job="+started.Job+" ") {
					count++
				}
			}
			if count != 1 || strings.Count(s.text(), notice) != 1 {
				t.Fatalf("not exactly once: ledger=%d screen=%d", count, strings.Count(s.text(), notice))
			}
			t.Logf("PID=%d retained across %d handoffs; status=%s; complete log; ledger notices=%d screen notices=1", pid, tc.handoffs, row.Status, count)
		})
	}
}

// Stage three intentionally replaces cross-name live upgrades with offline
// migration. Rejection must leave the retiring server's data and PTYs usable.
func TestLegacyLiveHandoffRefusedWithoutDamage(t *testing.T) {
	baseline := prepareMigrationBaseline(t)
	home, err := os.MkdirTemp("/tmp", "ih-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	config := filepath.Join(home, ".config/gtx-2e01183")
	if err = os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(config, "config.toml"), []byte("onboarding=false\n[experimental]\nallow_nested=true\n[session]\nresume_agents_on_restore=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(home, ".local/state/gtx-2e01183/stage3/gtx.sock")
	stop := migrationServer(t, baseline, home, socket)
	defer stop()
	var before map[string]any
	migrationRPC(t, socket, "pane.list", map[string]any{}, &before)
	var originalPanes struct {
		Panes []struct {
			PaneID string `json:"pane_id"`
		} `json:"panes"`
	}
	migrationRPC(t, socket, "pane.list", map[string]any{}, &originalPanes)
	if len(originalPanes.Panes) == 0 {
		t.Fatal("no panes before handoff")
	}
	migrationRPC(t, socket, "pane.report_agent", api.PaneReportAgentParams{PaneID: originalPanes.Panes[0].PaneID, Source: "reject", Agent: "codex", State: "idle"}, nil)
	var originalDecision api.DecisionResult
	migrationRPC(t, socket, "decision.create", api.DecisionCreateParams{Kind: "ask", Title: "survives-refusal", Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}}, &originalDecision)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(45 * time.Second))
	if err = json.NewEncoder(conn).Encode(map[string]any{"id": "reject", "method": "server.live_handoff", "params": map[string]any{"import_exe": binPath}}); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error *api.Error `json:"error"`
	}
	if err = json.NewDecoder(conn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if response.Error == nil || !strings.Contains(response.Error.Message, "offline migration") || strings.Contains(strings.ToLower(response.Error.Message), "gtx") {
		t.Fatalf("unclear rejection: %+v", response.Error)
	}
	var after map[string]any
	migrationRPC(t, socket, "pane.list", map[string]any{}, &after)
	oldRaw, _ := json.Marshal(before)
	newRaw, _ := json.Marshal(after)
	// Pane status metadata can change while shells run; stable identity and cwd must stay.
	var panes struct {
		Panes []struct {
			PaneID string `json:"pane_id"`
		} `json:"panes"`
	}
	migrationRPC(t, socket, "pane.list", map[string]any{}, &panes)
	if len(panes.Panes) == 0 {
		t.Fatal("panes lost", string(oldRaw), string(newRaw))
	}
	pane := panes.Panes[0].PaneID
	if len(panes.Panes) != len(originalPanes.Panes) || pane != originalPanes.Panes[0].PaneID {
		t.Fatal("pane identity changed")
	}
	var retained api.DecisionResult
	migrationRPC(t, socket, "decision.get", api.DecisionTargetParams{DecisionID: originalDecision.Decision.DecisionID}, &retained)
	if retained.Decision.Title != originalDecision.Decision.Title || retained.Decision.Status != "pending" {
		t.Fatal("DB decision changed", retained)
	}

	migrationRPC(t, socket, "pane.send_text", api.PaneSendTextParams{PaneID: pane, Text: "printf handoff-rejection-survived\n"}, nil)
	migrationRPC(t, socket, "pane.send_keys", api.PaneSendKeysParams{PaneID: pane, Keys: []string{"Enter"}}, nil)
	deadline := time.Now().Add(15 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		var read map[string]any
		migrationRPC(t, socket, "pane.read", api.PaneReadParams{PaneID: pane}, &read)
		raw, _ := json.Marshal(read)
		if strings.Contains(string(raw), "handoff-rejection-survived") {
			found = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatal("old pane stopped processing input")
	}
	migrationRPC(t, socket, "msg.send", api.MsgSendParams{Room: "reject", FromAgent: "proof", To: pane, Body: "after-rejection"}, nil)
	t.Log("old server API, pane output and database remain usable after explicit refusal")
}
