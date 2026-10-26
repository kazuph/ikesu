package runtime

import (
	"time"

	"github.com/kazuph/ikesu/internal/benchstage"
)

func (p *Pane) queueProcessChange() {
	if p.owner == nil {
		return
	}
	s := p.owner
	s.processMu.Lock()
	if s.processWork == nil {
		s.processWork = map[*Pane]bool{}
	}
	s.processWork[p] = true
	s.processMu.Unlock()
}

func (p *Pane) forgetProcessSchedule() {
	if p.guard != nil {
		p.guard.StopObserving()
	}
	if p.owner == nil {
		return
	}
	s := p.owner
	s.processMu.Lock()
	delete(s.processWork, p)
	delete(s.processTicks, p)
	delete(s.processDue, p)
	s.processMu.Unlock()
}

// PollPendingProcesses observes changed processes and the existing silent-cwd
// deadlines. A quiet pane is absent from the 20Hz process-state checks.
func (s *Supervisor) PollPendingProcesses(now time.Time) []int {
	s.processMu.Lock()
	if len(s.processWork) == 0 && len(s.processTicks) == 0 && (s.processNext.IsZero() || now.Before(s.processNext)) {
		s.processMu.Unlock()
		return nil
	}
	work := s.processWork
	if work == nil {
		work = map[*Pane]bool{}
	}
	s.processWork = nil
	for p := range s.processTicks {
		work[p] = true
	}
	if !s.processNext.IsZero() && !now.Before(s.processNext) {
		for p, at := range s.processDue {
			if !now.Before(at) {
				work[p] = true
			}
		}
	}
	if s.processTicks == nil {
		s.processTicks = map[*Pane]bool{}
		s.processDue = map[*Pane]time.Time{}
	}
	s.processMu.Unlock()
	defer benchstage.Span("runtime.process.pending")()
	benchstage.Values("runtime.process.targets", len(work), 0)
	var changed []int
	for p := range work {
		p.mu.Lock()
		dead := p.dead || p.retired.Load()
		p.mu.Unlock()
		if dead || s.Get(p.ID) != p {
			p.forgetProcessSchedule()
			continue
		}
		before := p.processRevision.Load()
		p.PollProcessChanged()
		if before != p.processRevision.Load() {
			changed = append(changed, p.ID)
		}
		p.mu.Lock()
		guarded := p.guard != nil && (p.effectiveAgent() != "" || p.guardStarted != "")
		settling := !p.processSettlingUntil.IsZero()
		at := p.processCwdAt.Add(foregroundJobCacheLifetime)
		p.mu.Unlock()
		if guarded && p.guard.ObserveChanges(p.queueProcessChange) {
			guarded = false
		}
		s.processMu.Lock()
		// Keep the original integrity-check cadence if native asset watches
		// cannot cover this pane. Successful watches queue only changed assets.
		if settling || guarded || !processEventQueue() {
			s.processTicks[p] = true
			delete(s.processDue, p)
		} else {
			delete(s.processTicks, p)
			s.processDue[p] = at
		}
		s.processMu.Unlock()
	}
	s.processMu.Lock()
	s.processNext = time.Time{}
	for _, at := range s.processDue {
		if s.processNext.IsZero() || at.Before(s.processNext) {
			s.processNext = at
		}
	}
	s.processMu.Unlock()
	return changed
}
