package cli

import (
	"bytes"
	"testing"

	"github.com/kazuph/ikesu/internal/machine"
)

func TestGlobalMachineErrorObservedRejections(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--machine", "a", "--machine", "b", "pane", "list"}, "--machine can only be specified once"},
		{[]string{"--machine", "a", "run", "--machine", "b", "--", "true"}, "--machine can only be specified once"},
		{[]string{"--machine=a", "--machine=b", "pane", "list"}, "--machine can only be specified once"},
		{[]string{"--machine", "a", "pane", "list", "--machine", "b"}, "--machine can only be specified once"},
		{[]string{"--remote", "h", "--machine", "a", "pane", "list"}, "--remote can only be used with the default launch command"},
		{[]string{"--machine", "a", "--remote", "h", "--help"}, "--machine cannot be used with --remote"},
		{[]string{"--machine"}, "missing value for --machine"},
		{[]string{"--machine="}, "--machine requires a saved machine label or id"},
		{[]string{"--machine", "--help"}, "--machine requires a saved machine label or id"},
		{[]string{"run", "--machine", "a", "--", "true"}, ""},
		{[]string{"--machine", "a", "pane", "list", "--", "--machine", "b"}, ""},
	} {
		if got := GlobalMachineError(tc.args); got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.args, got, tc.want)
		}
	}
}

func TestForwardMachineObservedErrors(t *testing.T) {
	t.Setenv("IKESU_STATE_DIR", t.TempDir())
	var out, errOut bytes.Buffer
	o := Options{Stdout: &out, Stderr: &errOut}
	c := (&runner{o: o}).catalog()
	p, err := c.AddVerified("h", "disabled", "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Change(p.ID, "disable", "", nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ref    string
		args   []string
		code   int
		stderr string
	}{
		{"a", []string{"run", "--", "true"}, 2, "error: --machine can only route agent, pane, workspace, or worktree\nrun 'ikesu --help' for usage\n"},
		{"a", []string{"inbox"}, 2, "error: --machine can only route agent, pane, workspace, or worktree\nrun 'ikesu --help' for usage\n"},
		{"a", []string{"machine", "list"}, 2, "error: --machine can only route agent, pane, workspace, or worktree\nrun 'ikesu --help' for usage\n"},
		{"a", nil, 2, "error: --machine requires a subcommand\nrun 'ikesu --help' for usage\n"},
		{"nosuch", []string{"pane", "list"}, 1, "error: machine \"nosuch\" was not found\n"},
		{"disabled", []string{"pane", "list"}, 1, "error: machine \"disabled\" is disabled; enable it before use\n"},
	} {
		out.Reset()
		errOut.Reset()
		code := ForwardMachine(tc.ref, tc.args, o)
		if code != tc.code || errOut.String() != tc.stderr || out.Len() != 0 {
			t.Fatalf("%q %q: code=%d stderr=%q", tc.ref, tc.args, code, errOut.String())
		}
	}
	profiles := []machine.Profile{{ID: "m_a", Label: "same", Enabled: true}, {ID: "m_b", Label: "same", Enabled: true}}
	if _, err := resolveForwardMachine(profiles, "same"); err == nil || err.Error() != `machine label "same" is ambiguous; use the profile id` {
		t.Fatal(err)
	}
}
