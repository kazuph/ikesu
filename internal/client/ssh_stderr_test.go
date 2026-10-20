package client

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestSSHStderrTailRetainsExactLast64KiBAcrossWraps(t *testing.T) {
	var tail sshStderrTail
	want := ""
	for _, size := range []int{0, 1, sshStderrLimit - 1, 17, sshStderrLimit, sshStderrLimit + 1, 3, 8192, 11} {
		text := strings.Repeat("0123456789", size/10) + "0123456789"[:size%10]
		tail.WriteString(text)
		want += text
		if len(want) > sshStderrLimit {
			want = want[len(want)-sshStderrLimit:]
		}
		if tail.String() != want || tail.size > sshStderrLimit || cap(tail.data) > sshStderrLimit {
			t.Fatal("retained diagnostics differ or exceed the review limit", size, tail.size, cap(tail.data))
		}
	}
}

func TestSSHStderrLongUnterminatedLineAndSplitApprovalPrefix(t *testing.T) {
	url := "https://login.tailscale.com/" + strings.Repeat("a", 70000)
	opened := []string{}
	s := &sshEndpoint{deadline: time.NewTimer(time.Hour), onApproval: func(u string) { opened = append(opened, u) }}
	defer s.deadline.Stop()
	// The prefix crosses ReadSlice's buffer boundary after an ordinary long line.
	input := strings.Repeat("x", 4*sshStderrLimit) + "\n" + strings.Repeat(" ", 4090) + sshApprovalPrefix + " " + url
	s.readStderr(strings.NewReader(input))
	if len(opened) != 1 || opened[0] != url {
		t.Fatal("split prefix or full URL lost", len(opened))
	}
	if s.stderr.String() != input[len(input)-sshStderrLimit:] || cap(s.stderr.data) != sshStderrLimit {
		t.Fatal("unterminated line accumulated outside the bounded diagnostic tail")
	}
}

// As authorized for approval tests, the SSH executable supplies real stderr
// input without opening the owner's browser or contacting their servers.
func TestSSHStderrProcessPreservesFullFinalErrorAndApprovalURL(t *testing.T) {
	teststate.Lock(t)
	dir := t.TempDir()
	url := "https://login.tailscale.com/" + strings.Repeat("b", 70000)
	finalError := "Permission denied: " + strings.Repeat("diagnostic detail ", 3000) + "END_ERROR"
	data := strings.Repeat("old diagnostic\n", 20000) + sshApprovalPrefix + " " + url + "\n" + finalError + "\n"
	file := filepath.Join(dir, "stderr")
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\n/bin/cat \"$GTX_TEST_SSH_STDERR\" >&2\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	teststate.Setenv(t, "GTX_TEST_SSH_STDERR", file)
	var opened []string
	s, err := openSSHEndpoint(machine.Profile{Target: "test-input.invalid"}, func(u string) { opened = append(opened, u) })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = io.ReadAll(s)
	if err == nil || !strings.Contains(err.Error(), finalError) {
		t.Fatal("final error was truncated", err)
	}
	if len(opened) != 1 || opened[0] != url {
		t.Fatal("full approval URL was lost", len(opened))
	}
	if s.stderr.String() != data[len(data)-sshStderrLimit:] || cap(s.stderr.data) != sshStderrLimit {
		t.Fatal("process diagnostics were not capped to their exact tail")
	}
	if s.cmd.ProcessState == nil || !s.cmd.ProcessState.Exited() {
		t.Fatal("owned SSH process did not exit")
	}
}
