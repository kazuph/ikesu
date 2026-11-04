package server

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kazuph/ikesu/internal/cli"
	"github.com/kazuph/ikesu/internal/teststate"
)

const guardStoppingTestLimit = 40

type countingGuardListener struct {
	net.Listener
	accepted atomic.Int32
}

func (l *countingGuardListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		if l.accepted.Add(1) > guardStoppingTestLimit {
			_ = c.Close()
		}
	}
	return c, err
}

func TestGuardStoppingRetryLimit(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "guard-stopping")
	path := filepath.Join(teststate.TempDir(t), "guard.sock")
	raw, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	listener := &countingGuardListener{Listener: raw}
	s.apiLn = listener
	s.closing.Store(1)
	done := make(chan struct{})
	go func() { s.apiLoop(); close(done) }()
	t.Cleanup(func() { _ = listener.Close(); <-done; s.apiWG.Wait(); s.sup.KillAll() })
	output := filepath.Join(t.TempDir(), "must-not-exist")
	teststate.Setenv(t, "IKESU_GUARD_MARKER", "/nonexistent/marker")
	teststate.Setenv(t, "IKESU_PANE_ID", "p1")
	teststate.Setenv(t, "IKESU_SOCKET_PATH", path)
	teststate.Setenv(t, "IKESU_SOCKET_PATH_EXPLICIT", "1")
	var diagnostic bytes.Buffer
	code := cli.Run([]string{"guard", "exec", "--", "/usr/bin/touch", output}, cli.Options{Stderr: &diagnostic})
	if got := listener.accepted.Load(); got != guardStoppingTestLimit {
		t.Fatalf("connections=%d want %d", got, guardStoppingTestLimit)
	}
	if code == 0 || !strings.Contains(diagnostic.String(), "server_stopping") {
		t.Fatalf("expected stopping error, got %v: %s", code, diagnostic.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("guard command executed or output could not be checked: %v", err)
	}
}
