package e2e

import (
	"bufio"
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRestartWaitsForOriginalServerConnectionToClose(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "original-pid")
	wrapper := filepath.Join(t.TempDir(), "original-server")
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\necho $$ > %q\nexec %q \"$@\"\n", pidfile, binPath)), 0700); err != nil {
		t.Fatal(err)
	}
	s := startVersionMode(t, nil, false, binPath, wrapper)
	s.waitFor("original shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.send("\x02v")
	s.waitFor("saved two panes", 5*time.Second, func(tx string) bool { return countPanes(tx) >= 2 })
	old, err := net.Dial("unix", api.APISocketPath(filepath.Join(s.stateDir, s.name)))
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	_ = old.SetDeadline(time.Now().Add(15 * time.Second))
	rd := bufio.NewReader(old)
	ping := api.Request{ID: "original-process", Method: api.MethodPing, Params: api.MarshalParams(api.EmptyParams{})}
	if err := api.WriteRequest(old, ping); err != nil {
		t.Fatal(err)
	}
	before, err := api.ReadResponse(rd)
	if err != nil || before.Err != nil {
		t.Fatal(before, err)
	}
	pid, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("original server PID=%s verified on retained Unix connection", strings.TrimSpace(string(pid)))
	if out, err := s.cli("server", "restart"); err != nil {
		t.Fatal(out, err)
	}
	// The original connection is an OS-owned identity, independent of pathname reuse.
	// Keeping it open also exercises the server's existing API drain boundary.
	if err := api.WriteRequest(old, ping); err == nil {
		if response, err := api.ReadResponse(rd); err == nil {
			t.Fatalf("restart returned while original PID %s still responds: %+v", strings.TrimSpace(string(pid)), response)
		}
	}
	out, err := s.cli("pane", "list")
	if err != nil || !strings.Contains(out, `"pane_id":"p1"`) || !strings.Contains(out, `"pane_id":"p2"`) {
		t.Fatal("successor not restored and ready", out, err)
	}
	t.Log("original connection closed; successor immediately reports both restored panes")
}
