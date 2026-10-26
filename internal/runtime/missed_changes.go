package runtime

func (p *Pane) keepMissedChange(kind EventKind) {
	if p.owner == nil {
		return
	}
	s := p.owner
	s.missedMu.Lock()
	defer s.missedMu.Unlock()
	if s.missed == nil {
		s.missed = map[*Pane]EventKind{}
	}
	if previous, ok := s.missed[p]; !ok || previous != EventExited {
		s.missed[p] = kind
	}
}

func (p *Pane) clearMissedChange() {
	if p.owner == nil {
		return
	}
	s := p.owner
	s.missedMu.Lock()
	// An earlier Changed event must not acknowledge a later lost exit.
	if s.missed[p] == EventChanged {
		delete(s.missed, p)
	}
	s.missedMu.Unlock()
}

// TakePendingChanges recovers only notifications that could not enter Events.
// It retains at most one entry per pane, without rescanning quiet panes.
func (s *Supervisor) TakePendingChanges() []Event {
	s.missedMu.Lock()
	if len(s.missed) == 0 {
		s.missedMu.Unlock()
		return nil
	}
	pending := s.missed
	s.missed = nil
	s.missedMu.Unlock()
	var out []Event
	for pane, kind := range pending {
		if s.Get(pane.ID) == pane {
			out = append(out, Event{Pane: pane.ID, Kind: kind})
		}
	}
	return out
}
