package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunMachineRoutingAndRefusal(t *testing.T) {
	t.Setenv("GTX_STATE_DIR", t.TempDir())
	var out, errOut bytes.Buffer
	o := Options{Stdout: &out, Stderr: &errOut}
	for _, args := range [][]string{{"run", "--sync", "--", "true"}, {"run", "--machine", "missing", "--", "true"}} {
		if code := Run(args, o); code != 2 {
			t.Fatal(code, errOut.String())
		}
		if strings.Contains(errOut.String(), "server_not_running") {
			t.Fatal("local dispatch attempted")
		}
	}
	r := &runner{o: o}
	_, err := r.catalog().AddVerified("mini", "remote", "default")
	if err != nil {
		t.Fatal(err)
	}
	argv, err := r.remoteRun("remote", false, []string{"printf", "a'b;$HOME"}, "~/scratch", false)
	if err != nil || argv[0] != "ssh" || argv[1] != "-n" {
		t.Fatal(argv, err)
	}
	if !strings.Contains(argv[len(argv)-1], "-lic") {
		t.Fatal(argv)
	}
}
