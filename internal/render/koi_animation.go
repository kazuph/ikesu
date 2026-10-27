package render

import "time"

// KoiAnimation belongs to a presenter, never to the shared server snapshot.
type KoiAnimation struct {
	epoch time.Time
	done  map[string]time.Time
}

func (a *KoiAnimation) Apply(in *Input, machines *MachineSidebar, now time.Time, expiry time.Duration) {
	if a.epoch.IsZero() {
		a.epoch = now
	}
	in.KoiTime = now.Sub(a.epoch).Seconds()
	if a.done == nil {
		a.done = map[string]time.Time{}
	}
	in.KoiGlow = map[string]bool{}
	present := map[string]bool{}
	for _, agent := range KoiAgentsFor(*in, machines) {
		if agent.State != "done" {
			continue
		}
		present[agent.ID] = true
		since, ok := a.done[agent.ID]
		if !ok {
			since = now
			a.done[agent.ID] = since
		}
		in.KoiGlow[agent.ID] = now.Sub(since) < expiry
	}
	for id := range a.done {
		if !present[id] {
			delete(a.done, id)
		}
	}
}
