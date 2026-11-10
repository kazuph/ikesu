package server

import (
	"os"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/teststate"
)

// 週1回の見直し: 番が来たら decision を1つ作り、同じ週には2回作らない。"off" なら作らない。
func TestGuardAuditWeeklyCreatesOneDecision(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "guard-audit")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	defer func() { s.app.Stop(); <-appDone }()
	root := api.StateDir()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local) // 火曜。月曜 10:00 の枠は済んだ後
	if err := guard.AppendAsk(root, guard.AskRecord{TS: now.UTC().Format(time.RFC3339), TSEpoch: now.Unix() - 3600, Rule: "http-write", Command: "curl -X POST https://e.com", Cwd: "/w", Outcome: guard.OutcomeApprove}); err != nil {
		t.Fatal(err)
	}
	count := func() (n int) {
		ds, err := store.Decisions("pending")
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range ds {
			if d.Kind == "ask" && d.MultiSelect {
				n++
			}
		}
		return
	}
	if err := s.guardAuditWeekly(now); err != nil || count() != 1 {
		t.Fatalf("first: %v count=%d", err, count())
	}
	if guard.ReadLastWeekly(root) != "2026-11-10" {
		t.Fatalf("last-weekly=%q", guard.ReadLastWeekly(root))
	}
	if _, err := os.Stat(guard.AuditDir(root) + "/2026-11-10.html"); err != nil {
		t.Fatal(err)
	}
	if err := s.guardAuditWeekly(now.Add(time.Hour)); err != nil || count() != 1 {
		t.Fatalf("second in the same week: %v count=%d", err, count())
	}
	if err := s.guardAuditWeekly(now.AddDate(0, 0, 5)); err != nil || count() != 1 { // 次の月曜 10:00 の前の土曜
		t.Fatalf("before next slot: %v count=%d", err, count())
	}
	if err := s.guardAuditWeekly(time.Date(2026, 11, 16, 10, 0, 0, 0, time.Local)); err != nil || count() != 2 {
		t.Fatalf("next week: %v count=%d", err, count())
	}
}
