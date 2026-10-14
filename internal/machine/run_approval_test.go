package machine

import (
	"bytes"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestObserveRunSharesPrefixAndChoosesSmallestSavedID(t *testing.T) {
	profiles := []Profile{{ID: "m2", Target: "same", Enabled: true}, {ID: "m1", Target: "same", Enabled: true}}
	for _, pane := range []bool{false, true} {
		argv := append([]string{"ssh"}, RunArgs(profiles[0], "/tmp", []string{"printf", "a'b"}, pane)...)
		before := append([]string{}, argv...)
		id, observed := ObserveRun(profiles, argv, "marker")
		if id != "m1" || !reflect.DeepEqual(argv, before) || !strings.Contains(observed[len(observed)-1], "marker") {
			t.Fatal(id, argv, observed)
		}
		shell := []string{"/bin/sh", "-c", "'true' && exec " + Command(argv)}
		id, observed = ObserveRun(profiles, shell, "marker")
		if id != "m1" {
			t.Fatal(id)
		}
		// Replace only the generated ssh prefix with printf to inspect the remote
		// argument via a real shell, proving quote concatenation preserves argv.
		prefix := Command(RunPrefix(profiles[0], pane))
		command := strings.Replace(observed[2], prefix, "'printf' '%s'", 1)
		output, err := exec.Command("/bin/sh", "-c", command).Output()
		want := "printf '%s\\n' 'marker' >&2; " + argv[len(argv)-1]
		if err != nil || string(output) != want {
			t.Fatal(string(output), want, err)
		}
	}
	ordinary := []string{"/usr/bin/env", "GTX_RUN_MACHINE_ID=m1", "true"}
	if id, argv := ObserveRun(profiles, ordinary, "marker"); id != "" || !reflect.DeepEqual(argv, ordinary) {
		t.Fatal(id, argv)
	}
}

func TestApprovalStreamPreservesStderrAndClearsOnConnection(t *testing.T) {
	t.Setenv("GTX_STATE_DIR", t.TempDir())
	// Existing record represents a browser already opened by another client.
	if err := claimApproval("m1", "old-url", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	state := &RunApproval{ID: "m1", Marker: "private-marker", Expire: func() {}}
	defer state.Close()
	s := &ApprovalStream{State: state, Out: &out}
	for _, text := range []string{"before\nTo authenticate, vis", "it: https://example.invalid/long\n", "private-", "marker\nafter\nlast"} {
		if _, err := s.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	if !state.connected || !state.awaiting {
		t.Fatal(state.connected, state.awaiting)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	want := "before\nTo authenticate, visit: https://example.invalid/long\nafter\nlast"
	if out.String() != want {
		t.Fatal(out.String())
	}
	opens := 0
	if err := claimApproval("m1", "next", func() error { opens++; return nil }); err != nil || opens != 1 {
		t.Fatal(opens, err)
	}
}

func TestRunPrivateMarkerCannotClearOtherMachineOrLeakOutput(t *testing.T) {
	t.Setenv("GTX_STATE_DIR", t.TempDir())
	for _, id := range []string{"m1", "m2"} {
		if err := claimApproval(id, "url", func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	profiles := []Profile{{ID: "m1", Target: "target", Enabled: true}}
	original := append([]string{"ssh"}, RunArgs(profiles[0], "/tmp", []string{"/bin/sh", "-c", "printf OUT; printf ERR >&2; exit 7"}, false)...)
	before := append([]string{}, original...)
	state, observed := PrepareRunApproval(profiles, original, func() { t.Error("unexpected expiry") })
	defer state.Close()
	var stdout, stderr bytes.Buffer
	out := &ApprovalStream{State: state, Out: &stdout}
	errOut := &ApprovalStream{State: state, Out: &stderr}
	forged := "gtx-ssh-connected-m2\n"
	if _, err := errOut.Write([]byte(forged)); err != nil {
		t.Fatal(err)
	}
	if state.connected {
		t.Fatal("forged marker connected")
	}
	command := exec.Command("/bin/sh", "-c", observed[len(observed)-1])
	command.Stdout, command.Stderr = out, errOut
	err := command.Run()
	if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 7 {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if err := errOut.Close(); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "OUT" || stderr.String() != forged+"ERR" {
		t.Fatal(stdout.String(), stderr.String())
	}
	if !reflect.DeepEqual(original, before) || strings.Contains(Command(original), state.Marker) {
		t.Fatal(original)
	}
	if _, err := os.Stat(approvalFile("m2")); err != nil {
		t.Fatal("other machine record removed", err)
	}
	if _, err := os.Stat(approvalFile("m1")); !os.IsNotExist(err) {
		t.Fatal("connected record remains", err)
	}
	// A remote PTY may merge stderr into stdout and translate LF to CRLF.
	state = &RunApproval{ID: "m1", Marker: "pty-marker", Expire: func() {}}
	out = &ApprovalStream{State: state, Out: &stdout}
	stdout.Reset()
	if _, err := out.Write([]byte("pty-marker\r\nPTY_OUTPUT")); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	state.Close()
	if stdout.String() != "PTY_OUTPUT" {
		t.Fatal(stdout.String())
	}
}
