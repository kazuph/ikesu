package render

import (
	"github.com/kazuph/ikesu/internal/layout"
	"math"
	"time"
)

const KoiRestInterval = 50 * time.Millisecond
const KoiFreeDotInterval = KoiRestInterval / 2
const KoiInterval = KoiFreeDotInterval

// Revision 3 timings are mirrored by the standalone brand site and checked
// against its embedded constants, without introducing an external asset load.
const KoiRippleLifetime = 0.8
const KoiGatherDuration = 1.5
const KoiAttractionLifetime = 2 * KoiGatherDuration
const KoiAttractionSpeedLimit = 4.0

type koiMotion struct {
	X, Y, T   float64
	Speed     float64
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
	speed := 1 / (KoiFreeDotInterval.Seconds() * math.Max(1, math.Hypot(rx*float64(cw), ry*float64(ch))/float64(scale)))
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
	attracting := false
	age := 0.0
	if r := in.KoiRipple; r != nil {
		age = max(0, in.KoiTime-r.Start)
		attracting = age < KoiGatherDuration
		tx := max(float64(pond.X), min(float64(pond.X+pond.W-w), r.X-float64(w)/2))
		ty := max(float64(pond.Y), min(float64(pond.Y+pond.H-h), r.Y-float64(h)/2))
		weight := 1.0
		if age > KoiGatherDuration {
			weight = (1 + math.Cos(math.Pi*min(1, (age-KoiGatherDuration)/KoiGatherDuration))) / 2
		}
		x += (tx - x) * weight
		y += (ty - y) * weight
		dx, dy = (tx-x)*float64(cw), (ty-y)*float64(ch)
		swimming = true
	}
	angle := math.Atan2(dy, dx)
	direction := int(math.Round(angle*16/(2*math.Pi))+16) % 16
	normalSpeed := 1 / KoiFreeDotInterval.Seconds()
	motionSpeed := normalSpeed
	if !swimming {
		motionSpeed = 0
	}
	if in.KoiMotion != nil {
		old, ok := in.KoiMotion[agent.ID]
		if ok && old.Pond == pond {
			dt := max(0, min(KoiInterval.Seconds(), in.KoiTime-old.T))
			dx, dy = (x-old.X)*float64(cw), (y-old.Y)*float64(ch)
			distance := math.Hypot(dx, dy)
			desiredSpeed := normalSpeed
			if !swimming {
				desiredSpeed = 0
			}
			acceleration := normalSpeed * KoiAttractionSpeedLimit / KoiGatherDuration
			if attracting {
				dots := distance / float64(scale)
				desiredSpeed = min(normalSpeed*KoiAttractionSpeedLimit, dots/max(KoiInterval.Seconds(), KoiGatherDuration-age), math.Sqrt(2*acceleration*dots))
			}
			motionSpeed = max(old.Speed-acceleration*dt, min(desiredSpeed, old.Speed+acceleration*dt))
			limit := dt * motionSpeed * float64(scale)
			if distance > limit && distance > 0 {
				x = old.X + dx/distance*limit/float64(cw)
				y = old.Y + dy/distance*limit/float64(ch)
			}
			if distance > 0 {
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
		in.KoiMotion[agent.ID] = koiMotion{X: x, Y: y, T: in.KoiTime, Speed: motionSpeed, Direction: direction, Pond: pond}
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

func (s KoiScene) UpdateInterval() time.Duration {
	if s.Ripple != nil {
		return KoiInterval
	}
	for _, fish := range s.Fish {
		if fish.Swimming {
			return KoiInterval
		}
	}
	return KoiRestInterval
}
