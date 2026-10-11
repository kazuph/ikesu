package server

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/session"
	"os"
	"strings"
)

func (s *Server) savePaneHistory(f *session.File) {
	if !s.cfg.Experimental.PaneHistory {
		return
	}
	h := session.History{Version: session.CurrentVersion, Panes: map[int]session.PaneHistory{}}
	f.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) {
		if rp := s.sup.Get(p.ID); rp != nil {
			rows, err := rp.ReadLines("recent", false)
			if err != nil {
				return
			}
			for len(rows) > 0 && strings.TrimSpace(rows[len(rows)-1]) == "" {
				rows = rows[:len(rows)-1]
			}
			h.Panes[p.ID] = session.PaneHistory{ANSI: strings.Join(rows, "\r\n"), Lines: len(rows)}
		}
	})
	if err := session.SaveHistory(s.dir, h); err != nil {
		fmt.Fprintf(os.Stderr, "gtx: save pane history: %v\n", err)
	}
}
func (s *Server) restorePaneHistory(f *session.File) {
	if !s.cfg.Experimental.PaneHistory {
		return
	}
	h, err := session.LoadHistory(s.dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gtx: read pane history: %v\n", err)
		return
	}
	if h == nil {
		return
	}
	f.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) {
		if s.cfg.ResumeAgentsOnRestore() && session.SafeSessionValue(p.SessionKind, p.SessionID) {
			if _, ok := s.buildResumeCommand(p.Agent, p.SessionKind, p.SessionID); ok {
				return
			}
		}
		if rp := s.sup.Get(p.ID); rp != nil {
			if record, ok := h.Panes[p.ID]; ok {
				rp.ReplayHistory(record.ANSI + "\r\n")
			}
		}
	})
}
