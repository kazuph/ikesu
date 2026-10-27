package render

import (
	"github.com/kazuph/ikesu/internal/layout"
	"math"
	"time"
)

const KoiInterval = 50 * time.Millisecond

// The site's ripple lifetime is three seconds. Gathering and release each use
// this same lifetime; it is independent of the completion glow expiry.
const KoiRippleLifetime = 3.0

type koiMotion struct {
	X, Y, T   float64
	Direction int
	Pond      layout.Rect
}

type KoiRipple struct{ X, Y, Start, Time float64 }

func koiPondPose(agent KoiAgent, pond layout.Rect, in Input) KoiFish {
	cw, ch := in.KoiCellW, in.KoiCellH
	if cw <= 0 {
		cw = 8
	}
	if ch <= 0 {
		ch = 16
	}
	w, h := 10, min(5, pond.H)
	scale := max(1, min(w*cw/40, h*ch/40))
	rx, ry := float64(pond.W-w)/2, float64(pond.H-h)/2
	// Bound the Euclidean source-dot velocity to one dot per existing tick.
	speed := 1 / (KoiInterval.Seconds() * math.Max(1, math.Hypot(rx*float64(cw), ry*float64(ch))/float64(scale)))
	phase := koiHash(agent.Seed+29) * 2 * math.Pi
	t := in.KoiTime
	swimming := agent.State == "working" || agent.State == "blocked"
	if !swimming {
		t = 0
	}
	u, v := t*speed+phase, t*speed+2*math.Pi*koiHash(agent.Seed+71)
	x := float64(pond.X) + rx*(1+math.Sin(u))
	y := float64(pond.Y) + ry*(1+math.Sin(v))
	dx, dy := rx*math.Cos(u)*float64(cw), ry*math.Cos(v)*float64(ch)
	if r := in.KoiRipple; r != nil {
		age := in.KoiTime - r.Start
		// The bounded homotopy returns to the original path after the shared
		// gathering/release window. No collision or neighbor force is used.
		weight := math.Sin(math.Pi * max(0, min(1, age/(2*KoiRippleLifetime))))
		tx := max(float64(pond.X), min(float64(pond.X+pond.W-w), r.X-float64(w)/2))
		ty := max(float64(pond.Y), min(float64(pond.Y+pond.H-h), r.Y-float64(h)/2))
		dx, dy = (tx-x)*float64(cw), (ty-y)*float64(ch)
		x += (tx - x) * weight
		y += (ty - y) * weight
		swimming = true
	}
	angle := math.Atan2(dy, dx)
	direction := int(math.Round(angle*16/(2*math.Pi))+16) % 16
	if in.KoiMotion != nil {
		old, ok := in.KoiMotion[agent.ID]
		if ok && old.Pond == pond {
			dt := max(0, min(KoiInterval.Seconds(), in.KoiTime-old.T))
			dx, dy = (x-old.X)*float64(cw), (y-old.Y)*float64(ch)
			distance := math.Hypot(dx, dy)
			limit := dt / KoiInterval.Seconds() * float64(scale)
			if distance > limit && distance > 0 {
				x = old.X + dx/distance*limit/float64(cw)
				y = old.Y + dy/distance*limit/float64(ch)
			}
			if math.Hypot(dx, dy) > 0 {
				desired := int(math.Round(math.Atan2(dy, dx)*16/(2*math.Pi))+16) % 16
				turn := (desired-old.Direction+24)%16 - 8
				direction = old.Direction
				if dt > 0 && turn != 0 {
					if turn > 0 {
						direction = (direction + 1) % 16
					} else {
						direction = (direction + 15) % 16
					}
				}
			} else {
				direction = old.Direction
			}
		}
		in.KoiMotion[agent.ID] = koiMotion{x, y, in.KoiTime, direction, pond}
	}
	depth := koiHash(agent.Seed + 17)
	if agent.State == "idle" || agent.State == "done" {
		depth += 1
	}
	if agent.State == "blocked" {
		depth = 0
	}
	wag := 0
	if swimming {
		wag = int(in.KoiTime*6) % 8
	}
	return KoiFish{Agent: agent, Rect: layout.Rect{X: int(x), Y: int(y), W: w, H: h}, Lane: pond, X: x, Y: y, Direction: direction, Wag: wag, Depth: depth, Swimming: swimming}
}

// AnimateKoi reuses the last composition's proven empty regions. No UI model,
// pane grid or server snapshot is composed on a decoration tick.
func AnimateKoi(scene KoiScene, in Input) *Frame {
	next := KoiScene{Pond: scene.Pond, Ripple: in.KoiRipple}
	for _, old := range scene.Fish {
		agent := old.Agent
		agent.Glow = in.KoiGlow[agent.ID]
		next.Fish = append(next.Fish, koiPondPose(agent, scene.Pond, in))
	}
	for _, old := range scene.Mini {
		next.Mini = append(next.Mini, koiPose(old.Agent, old.Lane, old.Rect.W, in.KoiTime))
	}
	f := &Frame{KoiScene: next}
	for _, fish := range next.Fish {
		f.KoiActive = f.KoiActive || fish.Swimming || fish.Agent.State == "blocked" || fish.Agent.Glow
	}
	f.KoiActive = f.KoiActive || len(next.Mini) > 0 || next.Ripple != nil
	drawKoiGraphics(f, next, in)
	return f
}
