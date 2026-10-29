package render

import (
	"math"
	"sort"

	"github.com/kazuph/ikesu/internal/layout"
)

// Keep admitted identities before considering newcomers; a removed agent
// remains resident until its entire rotation box has crossed the nearest edge.
func koiResidents(agents []KoiAgent, capacity int, pond layout.Rect, in Input) []KoiFish {
	return koiResidentsFor(agents, capacity, pond, in, "", 10, min(5, pond.H))
}

func koiResidentsFor(agents []KoiAgent, capacity int, pond layout.Rect, in Input, group string, w, h int) []KoiFish {
	pose := koiMotionPose
	if w == 6 && h == 1 {
		pose = koiBandPose
	}
	if in.KoiMotion == nil {
		var fish []KoiFish
		for _, agent := range agents[:min(capacity, len(agents))] {
			fish = append(fish, pose(agent, pond, in, w, h))
		}
		return fish
	}
	present := make(map[string]KoiAgent, len(agents))
	for _, agent := range agents {
		agent.Glow = in.KoiGlow[agent.ID]
		present[agent.ID] = agent
	}
	ids := make([]string, 0, len(in.KoiMotion))
	for id, m := range in.KoiMotion {
		if m.Agent.ID != "" && m.Group == group {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var fish []KoiFish
	for _, id := range ids {
		m := in.KoiMotion[id]
		agent, ok := present[id]
		if !ok {
			agent = m.Agent
		}
		if !ok && !m.Leaving {
			m.Leaving = true
			m.Entering = false
			m.ExitX, m.ExitY = koiExit(m.X, m.Y, w, h, pond, in)
			in.KoiMotion[id] = m
		}
		next := pose(agent, pond, in, w, h)
		if m.Leaving && math.Hypot(next.X-m.ExitX, next.Y-m.ExitY) < 1e-8 {
			delete(in.KoiMotion, id)
			continue
		}
		fish = append(fish, next)
		delete(present, id)
	}
	for _, agent := range agents {
		if len(fish) >= capacity {
			if agent.State != "blocked" || capacity == 0 {
				continue
			}
			blocked := false
			for _, f := range fish {
				blocked = blocked || f.Agent.State == "blocked"
			}
			if blocked {
				continue
			}
			victim := -1
			distance := math.Inf(1)
			resting := false
			for i, f := range fish {
				m := in.KoiMotion[f.Agent.ID]
				if m.Leaving || f.Agent.State == "blocked" {
					continue
				}
				x, y := koiExit(f.X, f.Y, f.Rect.W, f.Rect.H, pond, in)
				d := math.Hypot((x-f.X)*float64(max(1, in.KoiCellW)), (y-f.Y)*float64(max(1, in.KoiCellH)))
				rest := f.Agent.State == "idle" || f.Agent.State == "done"
				if victim < 0 || (rest && !resting) || (rest == resting && d < distance) {
					victim, distance, resting = i, d, rest
				}
			}
			if victim < 0 {
				continue
			}
			f := fish[victim]
			m := in.KoiMotion[f.Agent.ID]
			m.Leaving, m.Entering = true, false
			m.ExitX, m.ExitY = koiExit(f.X, f.Y, f.Rect.W, f.Rect.H, pond, in)
			in.KoiMotion[f.Agent.ID] = m
		}
		if _, ok := present[agent.ID]; !ok {
			continue
		}
		agent.Glow = in.KoiGlow[agent.ID]
		fish = append(fish, pose(agent, pond, in, w, h))
		m := in.KoiMotion[agent.ID]
		m.Group = group
		in.KoiMotion[agent.ID] = m
	}
	return fish
}

func koiExit(x, y float64, w, h int, pond layout.Rect, in Input) (float64, float64) {
	cw, ch := max(1, in.KoiCellW), max(1, in.KoiCellH)
	points := [][2]float64{{float64(pond.X - w), y}, {float64(pond.X + pond.W), y}, {x, float64(pond.Y - h)}, {x, float64(pond.Y + pond.H)}}
	if h == 1 {
		points = points[:2]
	}
	best := points[0]
	distance := math.Inf(1)
	for _, p := range points {
		d := math.Hypot((p[0]-x)*float64(cw), (p[1]-y)*float64(ch))
		if d < distance {
			distance, best = d, p
		}
	}
	return best[0], best[1]
}
