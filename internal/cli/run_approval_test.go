package cli

import (
	"github.com/kazuph/goterrace/internal/machine"
	"os/exec"
	"strings"
	"testing"
)

func TestPaneRemoteApprovalQuotePreservation(t *testing.T) {
	p := machine.Profile{ID: "m1", Target: "host", Enabled: true}
	for _, sync := range []bool{false, true} {
		argv := append([]string{"ssh"}, machine.RunArgs(p, "/tmp", []string{"printf", "a'b"}, true)...)
		if sync {
			argv = []string{"/bin/sh", "-c", "'true' && exec " + machine.Command(argv)}
		}
		original := shellCommandFromArgs(argv)
		id, observed := machine.ObservePaneRun([]machine.Profile{p}, original, "marker", shellCommandFromArgs)
		if id != "m1" {
			t.Fatal(sync, id, original)
		}
		prefix := shellCommandFromArgs(machine.RunPrefix(p, true))
		replacement := "printf '%s'"
		if sync {
			prefix = shellQuote(machine.Command(machine.RunPrefix(p, true)))
			prefix = strings.TrimSuffix(strings.TrimPrefix(prefix, "'"), "'")
			replacement = shellQuote("printf '%s'")
			replacement = strings.TrimSuffix(strings.TrimPrefix(replacement, "'"), "'")
		}
		command := strings.Replace(observed, prefix, replacement, 1)
		out, err := exec.Command("/bin/sh", "-c", command).CombinedOutput()
		remote := append([]string{"ssh"}, machine.RunArgs(p, "/tmp", []string{"printf", "a'b"}, true)...)
		want := "printf '%s\\n' 'marker' >&2; " + remote[len(remote)-1]
		if err != nil || string(out) != want {
			t.Fatal(sync, string(out), want, command, err)
		}
	}
}
