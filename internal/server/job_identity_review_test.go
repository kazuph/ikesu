package server

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestUnreapedChildRetainsStartIdentity(t *testing.T) {
	child := exec.Command("/bin/sh", "-c", "read value; exit 0")
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Wait()
	identity, err := jobProcessIdentity(child.Process.Pid)
	if err != nil {
		input.Close()
		t.Fatal(err)
	}
	input.Close()
	waitPromptCondition(t, func() bool {
		b, e := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(child.Process.Pid)).Output()
		return e == nil && strings.HasPrefix(strings.TrimSpace(string(b)), "Z")
	})
	actual, err := jobProcessIdentity(child.Process.Pid)
	if err != nil || actual != identity {
		t.Fatalf("unreaped child lost identity: got %q, want %q: %v", actual, identity, err)
	}
	if gone, err := monitorProcessGone(child.Process.Pid, identity); err != nil || !gone {
		t.Fatalf("zombie monitor not gone: %t %v", gone, err)
	}
}
