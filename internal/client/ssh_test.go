package client

import (
	"strings"
	"testing"
	"time"
)

func TestSSHApprovalPreservesFullURLAndNotifiesOncePerConnection(t *testing.T) {
	url := "https://login.tailscale.com/" + strings.Repeat("a", 70000)
	var opened []string
	s := &sshEndpoint{deadline: time.NewTimer(time.Hour), onApproval: func(url string) { opened = append(opened, url) }}
	defer s.deadline.Stop()
	before := time.Now()
	s.noteStderr("To authenticate, visit: " + url + "\n")
	if s.deadlineAt.Before(before.Add(30*time.Minute)) || s.deadlineAt.After(time.Now().Add(30*time.Minute)) {
		t.Fatal("approval did not replace the connect deadline with thirty minutes")
	}
	deadline := s.deadlineAt
	s.timeout()
	if s.expired {
		t.Fatal("connection timeout ended the active approval wait")
	}
	s.noteStderr("To authenticate, visit: " + url + "\n")
	if !s.deadlineAt.Equal(deadline) {
		t.Fatal("duplicate URL extended the approval deadline")
	}
	if len(opened) != 1 || opened[0] != url || !s.approval {
		t.Fatal("approval URL was truncated or delivered more than once", len(opened))
	}
	if !strings.Contains(s.stderr.String(), url) {
		t.Fatal("stderr lost the full URL")
	}
	s.connected()
	s.noteStderr("To authenticate, visit: https://example.invalid/after-ready\n")
	if len(opened) != 1 {
		t.Fatal("connected stream started another approval wait")
	}
}

func TestSSHApprovalParserDoesNotInventURL(t *testing.T) {
	for _, line := range []string{"Permission denied", "To authenticate, visit:\n", "https://example.invalid/"} {
		if got := approvalURL(line); got != "" {
			t.Fatal(line, got)
		}
	}
}
