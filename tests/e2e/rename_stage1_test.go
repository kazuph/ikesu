//go:build rename_stage1

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/platform"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// The pre-rename binary must be built from unchanged main 2e01183.
func TestRenameStageOneMixedHandoffAndRollback(t *testing.T) {
	old := os.Getenv("RENAME_OLD_BIN")
	if old == "" || filepath.Base(old) != "gtx" {
		t.Fatal("RENAME_OLD_BIN must point to main 2e01183 binary named gtx")
	}
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[ui]\nsidebar_width=23\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(t.TempDir(), "goterrace")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), "old-server")
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\ncd %q\nexec %q \"$@\"\n", cwd, old)), 0700); err != nil {
		t.Fatal(err)
	}
	s := startVersionMode(t, map[string]string{"GTX_CONFIG": cfg}, false, binPath, wrapper)
	s.waitFor("new client attached to old server", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	pidfile := filepath.Join(t.TempDir(), "identity")
	s.send(fmt.Sprintf("printf '%%s %%s\\n' $$ \"$PWD\" > %q; %q pane current >> %q; printf 'OLD_COMMAND_%%s\\n' READY\r", pidfile, old, pidfile))
	s.waitFor("old pane old CLI", 10*time.Second, func(text string) bool { return strings.Contains(text, "OLD_COMMAND_READY") })
	before, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), cwd) || !strings.Contains(string(before), `"pane_id":"p1"`) {
		t.Fatal(string(before))
	}
	var panes api.PaneListResult
	s.rpc(api.MethodPaneList, api.PaneListParams{}, &panes)
	paneID := "p1"
	var split api.PaneInfoResult
	// Wire fields remain the same, regardless of the client's new name.
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &paneID, Direction: "right", Focus: false}, &split)
	var prepared api.PaneListResult
	s.rpc(api.MethodPaneList, api.PaneListParams{}, &prepared)
	if len(prepared.Panes) != len(panes.Panes)+1 {
		t.Fatal(prepared)
	}
	var question api.DecisionResult
	s.rpc(api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "ask", Title: "RenamePending", Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}}, &question)
	for _, exe := range []string{binPath, old} {
		if out, err := s.cli("server", "live-handoff", "--import-exe", exe); err != nil {
			t.Fatalf("handoff to %s: %v %s", exe, err, out)
		}
		var current api.PaneListResult
		s.rpc(api.MethodPaneList, api.PaneListParams{}, &current)
		beforeIDs, afterIDs := []string{}, []string{}
		for _, p := range prepared.Panes {
			beforeIDs = append(beforeIDs, p.PaneID+":"+p.TerminalID+":"+p.WorkspaceID+":"+p.TabID)
		}
		for _, p := range current.Panes {
			afterIDs = append(afterIDs, p.PaneID+":"+p.TerminalID+":"+p.WorkspaceID+":"+p.TabID)
		}
		if !reflect.DeepEqual(beforeIDs, afterIDs) {
			t.Fatalf("IDs changed %v / %v", beforeIDs, afterIDs)
		}
		var retained api.DecisionResult
		s.rpc(api.MethodDecisionGet, api.DecisionTargetParams{DecisionID: question.Decision.DecisionID}, &retained)
		if retained.Decision.Status != "pending" {
			t.Fatal(retained)
		}
		marker := "AFTER_" + filepath.Base(exe) + "_HANDOFF"
		s.send(fmt.Sprintf("printf '%%s %%s\\n' $$ \"$PWD\" >> %q; %q pane current >> %q; printf 'AFTER_%%s_HANDOFF\\n' %q\r", pidfile, binPath, pidfile, filepath.Base(exe)))
		s.waitFor("old pane new CLI after handoff", 10*time.Second, func(text string) bool { return strings.Contains(text, marker) })
		data, err := os.ReadFile(pidfile)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if lines[0] != lines[len(lines)-2] {
			t.Fatalf("shell PID/cwd changed: %s", data)
		}
		var decoded struct {
			Result api.PaneCurrentResult `json:"result"`
		}
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Result.Pane.PaneID != "p1" {
			t.Fatal(decoded)
		}
		t.Logf("handoff to=%s retained IDs=%v shell=%s config=%s", exe, afterIDs, lines[0], cfg)
	}
	if out, err := s.cli("decision", "answer", question.Decision.DecisionID, "yes"); err != nil {
		t.Fatal(out, err)
	}
	var answered api.DecisionResult
	s.rpc(api.MethodDecisionGet, api.DecisionTargetParams{DecisionID: question.Decision.DecisionID}, &answered)
	if answered.Decision.Status != "answered" {
		t.Fatal(answered)
	}
}

func TestRenameStageOneChildCommandMatrix(t *testing.T) {
	old := os.Getenv("RENAME_OLD_BIN")
	if old == "" {
		t.Fatal("RENAME_OLD_BIN is required")
	}
	for _, pair := range []struct{ name, server, command string }{
		{"old-parent-new-command", old, binPath},
		{"new-parent-old-command", binPath, old},
		{"new-parent-new-command", binPath, binPath},
	} {
		t.Run(pair.name, func(t *testing.T) {
			s := startVersionMode(t, nil, false, binPath, pair.server)
			s.waitFor("shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
			root := t.TempDir()
			identity := filepath.Join(root, "identity.json")
			jobfile := filepath.Join(root, "job.json")
			answerfile := filepath.Join(root, "answer.json")
			s.send(fmt.Sprintf("%q pane current > %q; printf 'IDENTITY_%%s\\n' READY\r", pair.command, identity))
			s.waitFor("caller identity", 10*time.Second, func(text string) bool { return strings.Contains(text, "IDENTITY_READY") })
			raw, err := os.ReadFile(identity)
			if err != nil {
				t.Fatal(err)
			}
			var caller struct {
				Result api.PaneCurrentResult `json:"result"`
			}
			if err := json.Unmarshal(raw, &caller); err != nil || caller.Result.Pane.PaneID != "p1" {
				t.Fatal(string(raw), err)
			}
			s.send(fmt.Sprintf("%q pane report-agent p1 --source rename-child --agent codex --state idle --session-id rename-child-proof; %q run --label rename-child-job -- /bin/echo RENAME_CHILD_EXECUTED > %q\r", pair.command, pair.command, jobfile))
			s.waitFor("real run completion reaches caller", 15*time.Second, func(text string) bool { return strings.Contains(text, "run] exit=0 label=rename-child-job") })
			var info api.PaneInfoResult
			s.rpc(api.MethodPaneGet, api.PaneTarget{PaneID: "p1"}, &info)
			if info.Pane.AgentSession == nil || info.Pane.AgentSession.Value != "rename-child-proof" {
				t.Fatal(info)
			}
			var job struct {
				Job string `json:"job"`
			}
			raw, err = os.ReadFile(jobfile)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &job); err != nil || job.Job == "" {
				t.Fatal(string(raw), err)
			}
			if out, err := s.cli("log", job.Job); err != nil || !strings.Contains(out, "RENAME_CHILD_EXECUTED") {
				t.Fatal(out, err)
			}
			s.send(fmt.Sprintf("%q decision ask --title RenameChildQuestion --option yes:Yes:approve > %q\r", pair.command, answerfile))
			var pending api.DecisionListResult
			s.waitFor("real child question", 10*time.Second, func(string) bool {
				s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
				return len(pending.Decisions) == 1
			})
			if out, err := s.cli("decision", "answer", pending.Decisions[0].DecisionID, "yes"); err != nil {
				t.Fatal(out, err)
			}
			s.waitFor("answer returned to child CLI", 10*time.Second, func(string) bool {
				raw, _ = os.ReadFile(answerfile)
				return strings.Contains(string(raw), `"status":"answered"`)
			})
			t.Logf("%s: pane=%s report session=%s job=%s notice received; decision=%s answered", pair.name, caller.Result.Pane.PaneID, info.Pane.AgentSession.Value, job.Job, pending.Decisions[0].DecisionID)
		})
	}
}

func TestRenameStageOneImagesSocketsAndLocksAcrossRollback(t *testing.T) {
	old := os.Getenv("RENAME_OLD_BIN")
	if old == "" {
		t.Fatal("RENAME_OLD_BIN is required")
	}
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[experimental]\nkitty_graphics=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := startVersionMode(t, map[string]string{"GTX_CONFIG": cfg}, false, binPath, old)
	var outer kitty.Store
	s.emu.RegisterApcHandler(func(data []byte) bool {
		pos := s.emu.CursorPosition()
		_, _, _, handled := outer.Handle(data, pos.X, pos.Y, 0, 0, false)
		return handled
	})
	s.waitFor("shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.send("printf '\\033_Ga=T,t=d,f=24,s=1,v=1,i=32,c=3,r=2,C=1;AP8A\\033\\\\'; printf 'IMAGE_%s\\n' READY\r")
	s.waitFor("real SSH-free terminal image", 10*time.Second, func(string) bool { return len(outer.Placements) == 1 })
	initial := outer.Placements[0]
	if !bytes.Equal(initial.Image.Data, []byte{0, 255, 0}) {
		t.Fatal(initial)
	}
	socket := filepath.Join(s.stateDir, s.name, "gtx.sock")
	before, err := os.Stat(socket)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(s.stateDir, s.name, "gtx.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	for _, exe := range []string{binPath, old} {
		if out, err := s.cli("server", "live-handoff", "--import-exe", exe); err != nil {
			t.Fatal(exe, out, err)
		}
		after, err := os.Stat(socket)
		if err != nil || !os.SameFile(before, after) {
			t.Fatal("socket listener inode changed", err)
		}
		if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			if err == nil {
				_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
			}
			t.Fatalf("server lock not retained: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		duplicate := exec.CommandContext(ctx, binPath, "--session", s.name, "server")
		duplicate.Env = append(os.Environ(), "GTX_STATE_DIR="+s.stateDir)
		out, err := duplicate.CombinedOutput()
		cancel()
		if err == nil || !strings.Contains(string(out), "server is already running") {
			t.Fatalf("second server was not rejected: %v %s", err, out)
		}
		var tab api.TabCreatedResult
		s.rpc(api.MethodTabCreate, api.TabCreateParams{Focus: true}, &tab)
		s.waitFor("hide image", 10*time.Second, func(string) bool { return len(outer.Placements) == 0 })
		s.rpc(api.MethodTabFocus, api.TabTarget{TabID: "s1:t1"}, nil)
		s.waitFor("repaint retained image", 10*time.Second, func(string) bool { return len(outer.Placements) == 1 })
		current := outer.Placements[0]
		if current.Cols != initial.Cols || current.Rows != initial.Rows || !bytes.Equal(current.Image.Data, initial.Image.Data) {
			t.Fatalf("image changed after %s: %+v", exe, current)
		}
		t.Logf("handoff to %s: same socket inode, held lock, duplicate server rejected, repainted image=%x %dx%d cells", exe, current.Image.Data, current.Cols, current.Rows)
	}
}

// A baseline-to-baseline case separates an existing handoff limitation from
// a regression caused by the new module and command names.
func TestRenameStageOneJobsAcrossHandoff(t *testing.T) {
	old := os.Getenv("RENAME_OLD_BIN")
	if old == "" {
		t.Fatal("RENAME_OLD_BIN is required")
	}
	for _, pair := range []struct{ name, server, successor string }{
		{"unchanged-baseline", old, old},
		{"old-to-ikesu", old, binPath},
	} {
		t.Run(pair.name, func(t *testing.T) {
			s := startVersionMode(t, nil, false, binPath, pair.server)
			s.waitFor("shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
			s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "rename-job", Agent: "codex", State: "idle"}, nil)
			fifo := filepath.Join(t.TempDir(), "release")
			if err := unix.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			release, err := os.OpenFile(fifo, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer release.Close()
			var started api.RunStartedResult
			s.rpc(api.MethodRunStart, api.RunStartParams{CallerPane: "p1", Label: "rename-retained-job", Completion: "summary", Argv: []string{"/bin/sh", "-c", fmt.Sprintf("read value < %q; printf 'RENAME_JOB_FINISHED\\n'", fifo)}}, &started)
			store, err := dispatch.OpenReadOnly(filepath.Join(s.stateDir, "sessions", s.name, "gtx.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			initial, err := store.CommandRow(started.Job)
			if err != nil || initial == nil || initial.RunnerPID == nil || initial.Status != "running" {
				t.Fatal(initial, err)
			}
			pid := int(*initial.RunnerPID)
			t.Cleanup(func() {
				if strings.Contains(strings.Join(platform.ProcArgs(pid), " "), fifo) {
					_ = unix.Kill(-pid, unix.SIGKILL)
				}
			})
			if out, err := s.cli("server", "live-handoff", "--import-exe", pair.successor); err != nil {
				t.Fatal(out, err)
			}
			after, err := store.CommandRow(started.Job)
			if err != nil || after == nil || after.RunnerPID == nil || *after.RunnerPID != *initial.RunnerPID {
				t.Fatal("job identity lost", after, err)
			}
			if _, err := release.WriteString("continue\n"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { row, _ := store.CommandRow(started.Job); t.Logf("post-handoff job=%+v", row) })
			s.waitFor("retained job completion notification", 10*time.Second, func(text string) bool { return strings.Contains(text, "run] exit=0 label=rename-retained-job") })
			terminal, err := store.CommandRow(started.Job)
			if err != nil || terminal == nil || terminal.Status != "exited" || terminal.ExitCode == nil || *terminal.ExitCode != 0 {
				t.Fatal(terminal, err)
			}
			t.Logf("%s: job %s retained runner PID %d, exited=0, completion reached p1", pair.name, started.Job, pid)
		})
	}
}
