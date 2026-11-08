package server

import (
	"os"
	"path/filepath"

	"testing"

	"github.com/kazuph/ikesu/internal/teststate"
	"golang.org/x/sys/unix"
)

// reportedQuestionTerminal writes fixture updates through the real PTY so the
// supervisor delivers the same screen-change events as a running agent.
func reportedQuestionTerminal(t *testing.T) (*Server, *os.File) {
	t.Helper()
	s := newTestServer(t, "reported-question")
	teststate.Setenv(t, "SHELL", "/bin/sh")
	dir := t.TempDir()
	fifo := filepath.Join(dir, "output")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(fifo, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.sup.SpawnWith(1, dir, 160, 60, nil, []string{"/bin/sh", "-c", `stty raw -echo; exec cat "$1"`, "screen", fifo})
	if err != nil {
		output.Close()
		t.Fatal(err)
	}
	p.SetAgent("claude")
	s.initDecisionHub()
	s.app.SetQuitHook(s.stopDecisions)
	done := make(chan struct{})
	go func() { s.app.Run(); close(done) }()
	t.Cleanup(func() {
		s.app.Stop()
		<-done
		output.Close()
		s.sup.KillAll()
		if s.store != nil {
			s.store.Close()
		}
	})
	return s, output
}
