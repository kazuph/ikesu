package server

import (
	"fmt"
	"os"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
)

// guardAuditLoop は週1回のコマンド承認の見直しを、設定した曜日・時刻に出す。
// 起動した時にも見て、止まっていて時刻を過ぎていた週の分がまだなら出す。
func (s *Server) guardAuditLoop(stop <-chan struct{}) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	var lastErr string
	check := func() {
		if err := s.guardAuditWeekly(time.Now()); err != nil {
			if msg := err.Error(); msg != lastErr {
				lastErr = msg
				fmt.Fprintln(os.Stderr, "ikesu guard audit:", msg)
			}
		} else {
			lastErr = ""
		}
	}
	check()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			check()
		}
	}
}

// guardAuditWeekly は番が来ていれば見直しを作って decision にする。
func (s *Server) guardAuditWeekly(now time.Time) error {
	var c guard.Config
	ran := false
	s.app.Do(func() { ran, c = true, s.cfg.Guard })
	if !ran {
		return nil
	}
	w, err := guard.ParseWeekly(c.AuditWeekly)
	if err != nil {
		return err
	}
	root := api.StateDir()
	if !w.Due(now, guard.ReadLastWeekly(root)) {
		return nil
	}
	store, err := s.db()
	if err != nil {
		return err
	}
	decisions, err := store.Decisions("")
	if err != nil {
		return err
	}
	params, _, err := guard.WeeklyAudit(root, now, c, decisions)
	if err != nil || params == nil {
		return err
	}
	var createErr error
	created := false
	s.app.Do(func() {
		s.initDecisionHub()
		d, err := store.CreateDecision(*params, now)
		if err != nil {
			createErr = err
			return
		}
		created = true
		s.decisionChanged("decision_created", d)
		_ = s.expireDecisions(store)
	})
	if createErr != nil || !created {
		return createErr
	}
	return guard.MarkWeekly(root, now)
}
