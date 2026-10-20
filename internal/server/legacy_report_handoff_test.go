package server

import (
	"bytes"
	"encoding/gob"
	"testing"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

// These wire types deliberately omit Source and At, as the old server did.
type legacyReportTransfer struct {
	Agent, State string
	Unseen       bool
}
type legacyReportApp struct {
	State    *model.AppState
	Reported map[int]legacyReportTransfer
}
type legacyReportHandoff struct {
	App           legacyReportApp
	ReportSources map[int]string
}

func TestLegacyReportHandoffRestoresOfficialAuthority(t *testing.T) {
	teststate.Lock(t)
	for _, tc := range []struct{ source, want string }{
		{"gtx:opencode", "working"},
		{"custom:opencode", "blocked"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			s := newTestServer(t, "legacy-report")
			old := legacyReportHandoff{
				App: legacyReportApp{State: s.app.State(), Reported: map[int]legacyReportTransfer{
					1: {Agent: "opencode", State: "working", Unseen: true},
				}},
				ReportSources: map[int]string{1: tc.source},
			}
			var wire, reply, commit bytes.Buffer
			if err := gob.NewEncoder(&wire).Encode(old); err != nil {
				t.Fatal(err)
			}
			in := handoffImport{Encoder: gob.NewEncoder(&reply), Decoder: gob.NewDecoder(&commit)}
			if err := gob.NewDecoder(&wire).Decode(&in.State); err != nil {
				t.Fatal(err)
			}
			if r := in.State.App.Reported[1]; r.Source != "" || !r.At.IsZero() {
				t.Fatal("legacy gob unexpectedly supplied new fields", r)
			}
			if err := gob.NewEncoder(&commit).Encode(handoffResult{State: "commit"}); err != nil {
				t.Fatal(err)
			}
			hooks := 0
			s.app.SetAvailHook(func(int) { hooks++ })
			if err := s.restoreLive(&in, s.dir+"/gtx.sock"); err != nil {
				t.Fatal(err)
			}
			if hooks != 0 {
				t.Fatal("restoring authority fired a report hook")
			}
			restored := s.app.HandoffState().Reported[1]
			s.app.SetAvailHook(nil)
			if err := s.app.StartInitialPane(); err != nil {
				t.Fatal(err)
			}
			p := s.sup.Get(1)
			p.SetAgent("opencode")
			p.WithEmu(func(e *vt.Emulator) { e.Write([]byte("\x1b[2J\x1b[H△ Permission required")) })
			done := make(chan struct{})
			go func() { s.app.Run(); close(done) }()
			t.Cleanup(func() { s.app.Stop(); <-done; s.sup.KillAll() })
			waitPromptCondition(t, func() bool {
				visible := false
				s.app.Do(func() { visible = s.app.HandoffState().BlockerObservations[1].Visible })
				return visible
			})
			s.app.Do(func() {
				if got := s.app.PaneStatusKey(1); got != tc.want {
					t.Errorf("legacy %s with visible permission: got %s, want %s", tc.source, got, tc.want)
				}
				r := restored
				if r.Source != tc.source || r.Agent != "opencode" || r.State != "working" || !r.Unseen || !r.At.IsZero() {
					t.Errorf("restore changed report metadata: %+v", r)
				}
				s.app.SetDecisionAttention(map[int]bool{1: true})
				if s.app.PaneStatusKey(1) != "blocked" {
					t.Error("decision lost priority")
				}
				s.app.SetDecisionAttention(nil)
				if s.app.PaneStatusKey(1) != tc.want {
					t.Error("decision removal lost restored authority")
				}
			})
		})
	}
}
