package server

import (
	"github.com/kazuph/goterrace/internal/session"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"strings"
	"testing"
)

func TestPaneHistoryOptInSaveReplayAndNativeResumeExclusion(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "pane-history")
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.sup.KillAll)
	p := s.sup.Get(1)
	p.ReplayHistory("\x1b[31mHISTORY-MARKER\x1b[0m\r\n")
	f := s.app.State().Snapshot()
	s.savePaneHistory(f)
	h, err := session.LoadHistory(s.dir)
	if err != nil || h != nil {
		t.Fatal("off must not save", h, err)
	}
	s.cfg.Experimental.PaneHistory = true
	s.savePaneHistory(f)
	h, err = session.LoadHistory(s.dir)
	if err != nil || h == nil || !strings.Contains(h.Panes[1].ANSI, "HISTORY-MARKER") || !strings.Contains(h.Panes[1].ANSI, "\x1b[") {
		t.Fatal(h, err)
	}
	s.sup.Kill(1)
	s.app.RestoreSession(f)
	s.restorePaneHistory(f)
	lines, err := s.sup.Get(1).ReadLines("recent", true)
	if err != nil || !strings.Contains(strings.Join(lines, "\n"), "HISTORY-MARKER") {
		t.Fatal(lines, err)
	}
	s.sup.Kill(1)
	f.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) {
		p.Agent = "claude"
		p.SessionID = "exact-test"
		p.SessionKind = "id"
	})
	s.app.RestoreSession(f)
	s.restorePaneHistory(f)
	lines, err = s.sup.Get(1).ReadLines("recent", true)
	if err != nil || strings.Contains(strings.Join(lines, "\n"), "HISTORY-MARKER") {
		t.Fatal("native resume must exclude replay", lines, err)
	}
}
