package machine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSyncPriorStagesClearApprovalBeforeNextStage(t *testing.T) {
	t.Setenv("GTX_STATE_DIR", t.TempDir())
	if err := claimApproval("m1", "initial", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	record := approvalFile("m1")
	gate := filepath.Join(t.TempDir(), "approval")
	if err := syscall.Mkfifo(gate, 0600); err != nil {
		t.Fatal(err)
	}
	// Real shell subprocesses exercise command exit boundaries. Each stage checks
	// the durable record itself before proceeding; no SSH service is replaced.
	stages := [][]string{
		{"/bin/sh", "-c", "printf 'To authenticate, visit: https://example.invalid/first\\n' >&2; IFS= read -r approval < " + Quote(gate) + "; test \"$approval\" = approved"},
		{"/bin/sh", "-c", "test ! -e " + Quote(record) + " || exit 81; printf '{\"client\":\"sync\"}' > " + Quote(record) + "; printf 'To authenticate, visit: https://example.invalid/rsync\\n' >&2; exit 0"},
		{"/bin/sh", "-c", "test ! -e " + Quote(record) + " || exit 82; printf BODY; printf BODY_ERR >&2; exit 7"},
	}
	state := &RunApproval{ID: "m1", Marker: "unused", Expire: func() { t.Error("unexpected expiry") }}
	defer state.Close()
	argv, progress, err := state.SyncProgress([]string{"/bin/sh", "-c", syncRecipe(stages)})
	if err != nil || progress == nil {
		t.Fatal(progress, err)
	}
	var stdout, stderr bytes.Buffer
	out := &ApprovalStream{State: state, Out: &stdout}
	errOut := &ApprovalStream{State: state, Out: &stderr}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.ExtraFiles = progress.Child
	cmd.Stdout, cmd.Stderr = out, errOut
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	progress.Started(errOut)
	deadline := time.Now().Add(10 * time.Second)
	for {
		state.mu.Lock()
		waiting := state.awaiting
		state.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatal("prior stage never entered approval wait")
		}
		time.Sleep(time.Millisecond)
	}
	state.expire(time.Now().Add(15 * time.Second))
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("approval wait ended at connect deadline", err)
	}
	grant, err := os.OpenFile(gate, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintln(grant, "approved")
	grant.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	_ = progress.Close()
	_ = out.Close()
	_ = errOut.Close()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 7 {
		t.Fatal(err)
	}
	if stdout.String() != "BODY" || stderr.String() != "To authenticate, visit: https://example.invalid/first\nTo authenticate, visit: https://example.invalid/rsync\nBODY_ERR" {
		t.Fatal(stdout.String(), stderr.String())
	}
	if state.awaiting {
		t.Fatal("prior-stage approval was not reset")
	}
}

func TestSyncFailedApprovalKeepsRecordAndDoesNotExecuteBody(t *testing.T) {
	t.Setenv("GTX_STATE_DIR", t.TempDir())
	if err := claimApproval("m1", "waiting", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	body := filepath.Join(t.TempDir(), "body")
	stages := [][]string{{"/bin/sh", "-c", "printf 'To authenticate, visit: https://example.invalid/denied\\n' >&2; exit 255"}, {"/bin/sh", "-c", "touch " + Quote(body)}}
	state := &RunApproval{ID: "m1", Marker: "unused", Expire: func() {}}
	defer state.Close()
	argv, progress, err := state.SyncProgress([]string{"/bin/sh", "-c", syncRecipe(stages)})
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	stream := &ApprovalStream{State: state, Out: &stderr}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.ExtraFiles = progress.Child
	cmd.Stderr = stream
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	progress.Started(stream)
	err = cmd.Wait()
	_ = progress.Close()
	_ = stream.Close()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 255 {
		t.Fatal(err)
	}
	if _, err := os.Stat(body); !os.IsNotExist(err) {
		t.Fatal("body executed", err)
	}
	if _, err := os.Stat(approvalFile("m1")); err != nil {
		t.Fatal("failed wait lost record", err)
	}
	if err := ClearApproval("m1"); err != nil {
		t.Fatal(err)
	}
	opens := 0
	if err := claimApproval("m1", "retried", func() error { opens++; return nil }); err != nil || opens != 1 {
		t.Fatal(opens, err)
	}
}

func TestRunApprovalTimeoutIsThirtyMinutesAndStopsAfterSuccess(t *testing.T) {
	t.Setenv("GTX_STATE_DIR", t.TempDir())
	if err := claimApproval("m1", "waiting", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	expired := 0
	state := &RunApproval{ID: "m1", Marker: "connected", Expire: func() { expired++ }}
	defer state.Close()
	var output bytes.Buffer
	s := &ApprovalStream{State: state, Out: &output}
	before := time.Now()
	fmt.Fprintln(s, "To authenticate, visit: https://example.invalid/wait")
	if state.deadlineAt.Before(before.Add(30*time.Minute)) || state.deadlineAt.After(time.Now().Add(30*time.Minute)) {
		t.Fatal(state.deadlineAt)
	}
	state.expire(before.Add(15 * time.Second))
	if expired != 0 {
		t.Fatal("fifteen-second timeout ended approval")
	}
	state.expire(state.deadlineAt)
	if expired != 1 {
		t.Fatal(expired)
	}
	fmt.Fprintln(s, "connected")
	state.expire(state.deadlineAt.Add(time.Second))
	if expired != 1 {
		t.Fatal("connected command expired")
	}
	if strings.Contains(output.String(), "connected") {
		t.Fatal(output.String())
	}
}

func TestLiteralRecipeRejectsUnownedShellSyntax(t *testing.T) {
	for _, text := range []string{"'true' && touch /tmp/unowned", "'true' && exec $(echo ssh)", "'true' && exec 'ssh' | 'cat'"} {
		if _, ok := syncStages([]string{"/bin/sh", "-c", text}); ok {
			t.Fatal(text)
		}
	}
	words := []string{"ssh", "a'b", "$HOME; && exec 'no'", ""}
	decoded, ok := DecodeCommand(Command(words))
	if !ok || Command(decoded) != Command(words) {
		t.Fatal(decoded, ok)
	}
}
