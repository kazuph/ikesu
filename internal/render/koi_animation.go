package render

import (
	"time"
)

// KoiAnimation belongs to a presenter, never to the shared server snapshot.
type KoiAnimation struct {
	epoch       time.Time
	done        map[string]time.Time
	ripple      *KoiRipple
	freshRipple bool
	motion      map[string]koiMotion
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
		if a.freshRipple {
			a.ripple.Start = in.KoiTime
			a.freshRipple = false
		}
		if in.KoiTime-a.ripple.Start < KoiAttractionLifetime {
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
	active := map[string]bool{}
	for _, agent := range KoiAgentsFor(*in, machines) {
		active[agent.ID] = true
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
	// Rebuild on removal so the map's buckets cannot retain a historic peak.
	for id := range a.motion {
		if !active[id] {
			live := make(map[string]koiMotion, len(active))
			for id, m := range a.motion {
				if active[id] {
					live[id] = m
				}
			}
			a.motion = live
			in.KoiMotion = live
			break
		}
	}
	for id := range a.done {
		if !present[id] {
			live := make(map[string]time.Time, len(present))
			for id, since := range a.done {
				if present[id] {
					live[id] = since
				}
			}
			a.done = live
			break
		}
	}
}

// Click accepts only the unoccupied pond from the last actual composition.
func (a *KoiAnimation) Click(scene KoiScene, enabled bool, x, y int, now time.Time) bool {
	pond := scene.Pond
	if !enabled || len(scene.Fish) == 0 {
		return false
	}
	if pond.W == 0 || pond.H == 0 || x < pond.X || y < pond.Y || x >= pond.X+pond.W || y >= pond.Y+pond.H {
		return false
	}
	if a.epoch.IsZero() {
		a.epoch = now
	}
	a.freshRipple = true
	a.ripple = &KoiRipple{X: float64(x) + .5, Y: float64(y) + .5, Start: now.Sub(a.epoch).Seconds()}
	return true
}
