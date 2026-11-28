package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMachineListAndRejections(t *testing.T) {
	t.Setenv("IKESU_STATE_DIR", t.TempDir())
	var out, errOut bytes.Buffer
	o := Options{Stdout: &out, Stderr: &errOut}
	for _, args := range [][]string{{"machine", "list"}, {"machine", "list", "--json"}} {
		out.Reset()
		if Run(args, o) != 0 {
			t.Fatal(errOut.String())
		}
		if out.String() != "No saved SSH machines.\n" && out.String() != "[]\n" {
			t.Fatal(out.String())
		}
	}
	r := &runner{o: o}
	c := r.catalog()
	p, err := c.AddVerified("mini", "mini", "default")
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if Run([]string{"machine", "list"}, o) != 0 || !strings.Contains(out.String(), p.ID+"\tmini\tmini\tdefault\tenabled") {
		t.Fatal(out.String())
	}
	for _, args := range [][]string{{"machine", "add", "mini", "--label", "mini"}, {"machine", "add", "mini", "--label", "other"}, {"machine", "remove", "unknown"}, {"machine", "disable", "unknown"}, {"machine", "rename", "unknown", "--label", "x"}} {
		if Run(args, o) == 0 {
			t.Fatal(args)
		}
	}
	if len(c.Load()) != 1 {
		t.Fatal(c.Load())
	}
	os.WriteFile(filepath.Join(filepath.Dir(c.Path), "machines.json"), []byte("{"), 0600)
	out.Reset()
	if Run([]string{"machine", "list"}, o) != 0 || out.String() != "No saved SSH machines.\n" {
		t.Fatal(out.String())
	}
}

func TestMachineForwardRejectsBeforeLocalDispatch(t *testing.T) {
	t.Setenv("IKESU_STATE_DIR", t.TempDir())
	var out, errOut bytes.Buffer
	o := Options{Stdout: &out, Stderr: &errOut}
	c := (&runner{o: o}).catalog()
	p, err := c.AddVerified("mini", "mini", "default")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"machine", "list"}, {"inbox"}, {"tab", "list"}, {"pane", "list", "--remote", "mini"}} {
		if code := ForwardMachine("mini", args, o); code != 2 {
			t.Fatal(args, code)
		}
	}
	c.Change(p.ID, "disable", "", nil)
	if code := ForwardMachine("mini", []string{"pane", "list"}, o); code != 1 {
		t.Fatal(code)
	}
	if code := ForwardMachine("unknown", []string{"pane", "list"}, o); code != 1 {
		t.Fatal(code)
	}
	out.Reset()
	if code := ForwardMachine("unknown", []string{"pane", "list", "--help"}, o); code != 0 || !strings.Contains(out.String(), "# ikesu") {
		t.Fatal(code, out.String())
	}
}

func TestMachineConnectAndDisconnect(t *testing.T) {
	t.Setenv("IKESU_STATE_DIR", t.TempDir())
	var out, errOut bytes.Buffer
	o := Options{Stdout: &out, Stderr: &errOut}
	catalog := (&runner{o: o}).catalog()
	profile, err := catalog.AddVerified("host", "Remote", "work")
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		out.Reset()
		errOut.Reset()
		if Run(append([]string{"machine"}, args...), o) != 0 {
			t.Fatalf("%v: %s", args, errOut.String())
		}
		return out.String()
	}
	check := func(enabled bool, state string) {
		t.Helper()
		want := profile
		want.Enabled = enabled
		got := catalog.Load()
		if len(got) != 1 || got[0] != want {
			t.Fatalf("saved machine = %+v, want %+v", got, want)
		}
		if output := run("list"); !strings.HasSuffix(output, "\t"+state+"\n") {
			t.Fatalf("list = %q, want state %q", output, state)
		}
	}
	run("disconnect", profile.Label)
	check(false, "disconnected")
	run("connect", profile.ID)
	check(true, "enabled")
	run("disable", profile.ID)
	check(false, "disconnected")
	run("enable", profile.Label)
	check(true, "enabled")
	if help := run("help"); !strings.Contains(help, "machine connect <label-or-id>") || !strings.Contains(help, "machine disconnect <label-or-id>") {
		t.Fatal(help)
	}
}

func TestMachineRemoteConflict(t *testing.T) {
	for _, args := range [][]string{{"--remote", "host", "--machine", "saved", "pane", "list"}, {"--machine=saved", "--remote=host"}} {
		if !MachineRemoteConflict(args) {
			t.Fatal(args)
		}
	}
	if MachineRemoteConflict([]string{"run", "--machine", "saved", "--", "echo", "--remote"}) {
		t.Fatal("command arguments are not global options")
	}
}
