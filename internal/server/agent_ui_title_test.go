package server

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
	"testing"
)

func TestG5ReportedTitleTrimPreserveLabelClearAndSession(t *testing.T) {
	s := newTestServer(t, "titles")
	s.app.ExecCmd(model.CmdRenamePane{Pane: 1, Label: "reviewer"})
	title := " restore pane sessions "
	response := s.paneReportAgent("title", api.PaneReportAgentParams{PaneID: "p1", Source: "test", Agent: "codex", State: "working", Title: &title})
	if response.Err != nil {
		t.Fatal(response.Err)
	}
	rec, ok := s.app.SessionRecord(1)
	info, _ := s.paneInfo(1)
	if !ok || rec.Title != "restore pane sessions" || s.app.State().PaneLabels[1] != "reviewer" || info.Title == nil || *info.Title != rec.Title {
		t.Fatal(rec, info)
	}
	sid := "safe-session"
	s.paneReportAgent("session", api.PaneReportAgentParams{PaneID: "p1", Source: "test", Agent: "codex", State: "idle", SessionID: &sid})
	rec, _ = s.app.SessionRecord(1)
	if rec.Title != "restore pane sessions" || rec.Value != sid {
		t.Fatal("omitted title changed", rec)
	}
	title = "  "
	s.paneReportAgent("clear", api.PaneReportAgentParams{PaneID: "p1", Source: "test", Agent: "codex", State: "idle", Title: &title})
	rec, _ = s.app.SessionRecord(1)
	info, _ = s.paneInfo(1)
	if rec.Title != "" || rec.Value != sid || info.Title != nil || s.app.State().PaneLabels[1] != "reviewer" {
		t.Fatal("blank clear", rec, info)
	}
}
