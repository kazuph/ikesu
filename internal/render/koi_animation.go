package render

import (
	"github.com/kazuph/ikesu/internal/layout"
	"time"
)

// KoiAnimation belongs to a presenter, never to the shared server snapshot.
type KoiAnimation struct {
	epoch  time.Time
	done   map[string]time.Time
	ripple *KoiRipple
	motion map[string]koiMotion
}

func (a *KoiAnimation) Apply(in *Input, machines *MachineSidebar, now time.Time, expiry time.Duration) {
	if a.epoch.IsZero() {
		a.epoch = now
	}
	in.KoiTime = now.Sub(a.epoch).Seconds()
	if a.motion == nil {
		a.motion = map[string]koiMotion{}
	}
	in.KoiMotion = a.motion
	in.KoiRipple = nil
	if a.ripple != nil {
		if in.KoiTime-a.ripple.Start < 2*KoiRippleLifetime {
			r := *a.ripple
			r.Time = in.KoiTime
			in.KoiRipple = &r
		} else {
			a.ripple = nil
		}
	}
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

// Click accepts only the unoccupied pond from the last actual composition.
func (a *KoiAnimation) Click(pond layout.Rect, x, y int, now time.Time) bool {
	if pond.W == 0 || pond.H == 0 || x < pond.X || y < pond.Y || x >= pond.X+pond.W || y >= pond.Y+pond.H {
		return false
	}
	if a.epoch.IsZero() {
		a.epoch = now
	}
	a.ripple = &KoiRipple{X: float64(x) + .5, Y: float64(y) + .5, Start: now.Sub(a.epoch).Seconds()}
	return true
}
