//go:build machine_parity

package machine_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGlobalMachineReferenceParity compares the two real CLI processes.
// Run on mini with GTX_MACHINE_PARITY_BIN and GTX_MACHINE_REFERENCE_BIN;
// a missing prerequisite fails rather than substituting captured output.
func TestGlobalMachineReferenceParity(t *testing.T) {
	gtx := requiredBinary(t, "GTX_MACHINE_PARITY_BIN")
	reference := requiredBinary(t, "GTX_MACHINE_REFERENCE_BIN")
	root := t.TempDir()
	config := filepath.Join(root, "config")
	state := filepath.Join(root, "state")
	profiles := []map[string]any{
		{"id": "m_disabled", "label": "disabled", "target": "ssh://127.0.0.1:1", "session": "default", "enabled": false},
		{"id": "m_first", "label": "dupe", "target": "ssh://127.0.0.1:1", "session": "a", "enabled": true},
		{"id": "m_second", "label": "dupe", "target": "ssh://127.0.0.1:1", "session": "b", "enabled": true},
	}
	raw, err := json.Marshal(profiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(config, "herdr", "sessions", "compare"), filepath.Join(state, "compare")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "machines.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(), "XDG_CONFIG_HOME="+config, "XDG_STATE_HOME="+state, "HERDR_STATE_DIR="+filepath.Join(root, "herdr-state"), "GTX_STATE_DIR="+state, "GTX_CONFIG="+filepath.Join(root, "gtx-config.toml"), "HERDR_SOCKET_PATH=", "GTX_SOCKET_PATH=", "HERDR_PANE_ID=", "GTX_PANE_ID=")
	cases := []struct {
		name string
		args []string
	}{
		{"global_run", []string{"--machine", "nosuch", "run", "--", "true"}},
		{"global_run_command_machine_literal", []string{"--machine", "nosuch", "run", "--", "echo", "--machine"}},
		{"global_and_run_machine_twice", []string{"--machine", "nosuch", "run", "--machine", "x", "--", "true"}},
		{"global_machine_twice", []string{"--machine", "a", "--machine", "b", "pane", "list"}},
		{"equals_machine_twice", []string{"--machine=a", "--machine=b", "pane", "list"}},
		{"machine_twice_after_subcommand", []string{"--machine", "a", "pane", "list", "--machine", "b"}},
		{"machine_twice_missing_second_value", []string{"--machine", "a", "--machine"}},
		{"machine_twice_help", []string{"--machine", "a", "--machine", "b", "--help"}},
		{"unsupported_inbox", []string{"--machine", "nosuch", "inbox"}},
		{"machine_management", []string{"--machine", "nosuch", "machine", "list"}},
		{"unknown", []string{"--machine", "nosuch", "pane", "list"}},
		{"disabled", []string{"--machine", "disabled", "pane", "list"}},
		{"ambiguous", []string{"--machine", "dupe", "pane", "list"}},
		{"remote_first", []string{"--remote", "host", "--machine", "nosuch", "pane", "list"}},
		{"remote_after_machine", []string{"--machine", "nosuch", "--remote", "host", "pane", "list"}},
		{"remote_after_command", []string{"--machine", "nosuch", "pane", "list", "--remote", "host"}},
		{"remote_without_command", []string{"--machine", "nosuch", "--remote", "host"}},
		{"remote_help", []string{"--machine", "nosuch", "--remote", "host", "--help"}},
		{"remote_and_duplicate", []string{"--remote", "h", "--machine", "a", "--machine", "b"}},
		{"missing_value", []string{"--machine"}},
		{"empty_value", []string{"--machine="}},
		{"flag_as_value", []string{"--machine", "--help"}},
		{"missing_subcommand", []string{"--machine", "nosuch"}},
		{"local_help", []string{"--machine", "nosuch", "pane", "list", "--help"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--session", "compare"}, tc.args...)
			refCode, refErr, refOut := invoke(t, reference, args, env)
			code, stderr, stdout := invoke(t, gtx, args, env)
			wantErr := strings.ReplaceAll(refErr, "herdr", "gtx")
			t.Logf("argv=%q reference_exit=%d gtx_exit=%d reference_stderr=%q gtx_stderr=%q", tc.args, refCode, code, refErr, stderr)
			if code != refCode || stderr != wantErr {
				t.Fatalf("reference code=%d stderr=%q; gtx code=%d stderr=%q", refCode, wantErr, code, stderr)
			}
			if refCode != 0 && (refOut != "" || stdout != "") {
				t.Fatalf("rejection printed stdout: reference=%q gtx=%q", refOut, stdout)
			}
			if refCode == 0 && (!strings.Contains(refOut, "# herdr") || !strings.Contains(stdout, "# gtx")) {
				t.Fatal("local help absent")
			}
		})
	}
	// The installed reference lacks this fork's remote-run entry point;
	// compare only P2 above. Guard the existing gtx G9 path independently.
	code, stderr, _ := invoke(t, gtx, []string{"--session", "compare", "run", "--machine", "nosuch", "--", "true"}, env)
	if code != 2 || stderr != "remote run: machine \"nosuch\" was not found\n" {
		t.Fatalf("run --machine changed: code=%d stderr=%q", code, stderr)
	}
}

func requiredBinary(t *testing.T, key string) string {
	t.Helper()
	path := os.Getenv(key)
	if path == "" {
		t.Fatalf("%s is required", key)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func invoke(t *testing.T, binary string, args, env []string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return code, stderr.String(), stdout.String()
}
