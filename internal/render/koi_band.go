package render

import (
	"math"

	"github.com/kazuph/ikesu/internal/layout"
)

// The fastest visible crossing takes two existing gather periods, unless the
// source-dot limit requires more time. Resting fish retain the 18% pace.
const koiBandTraverseSeconds = 2 * KoiGatherDuration

func koiBandPose(agent KoiAgent, lane layout.Rect, in Input, w, h int) KoiFish {
	cw, ch := in.KoiCellW, in.KoiCellH
	if cw <= 0 {
		cw = 8
	}
	if ch <= 0 {
		ch = 16
	}
	_, _, scale := koiPixelGrid(w*cw, h*ch)
	maxSpeed := float64(scale) / KoiInterval.Seconds()
	pace := 1.0
	if agent.State == "idle" || agent.State == "done" {
		pace = 0.18
	}
	// Preserve koiPose's individual pace range so simultaneous entrants do not
	// remain stacked for their entire lifetime. Each resident's speed is constant.
	individualPace := (0.18 + koiHash(agent.Seed+11)*0.12) / (0.18 + 0.12)
	speed := math.Min(maxSpeed, float64(max(0, lane.W-w)*cw)/koiBandTraverseSeconds) * pace * individualPace
	if in.KoiMotion == nil {
		span := float64(max(0, lane.W-w))
		x, direction := float64(lane.X), 0
		if span > 0 {
			phase := math.Mod(koiHash(agent.Seed+29)*2*span+in.KoiTime*speed/float64(cw), 2*span)
			if phase >= span {
				phase, direction = 2*span-phase, 1
			}
			x += phase
		}
		return KoiFish{Agent: agent, Rect: layout.Rect{X: int(x), Y: lane.Y, W: w, H: h}, Lane: lane, X: x, Y: float64(lane.Y), Direction: direction, Wag: int(in.KoiTime*6) % 8, Swimming: true}
	}
	old, exists := in.KoiMotion[agent.ID]
	if !exists {
		old = koiMotion{X: float64(lane.X + lane.W), Y: float64(lane.Y), T: in.KoiTime, Direction: 1, Agent: agent, Pond: lane, Entering: true}
	}
	dt := max(0, min(KoiInterval.Seconds(), in.KoiTime-old.T))
	left, right := float64(lane.X), float64(lane.X+max(0, lane.W-w))
	direction := old.Direction
	if direction != 1 {
		direction = 0
	}
	targetX, targetY := right, float64(lane.Y)
	if old.Entering {
		targetX = right
		speed = maxSpeed
		direction = 1
	} else if old.Leaving {
		targetX, targetY = old.ExitX, old.ExitY
		speed = maxSpeed
	} else {
		if old.X <= left {
			direction = 0
		} else if old.X >= right {
			direction = 1
		}
		if direction == 1 {
			targetX = left
		}
	}
	dx, dy := (targetX-old.X)*float64(cw), (targetY-old.Y)*float64(ch)
	distance := math.Hypot(dx, dy)
	fraction := 1.0
	if distance > dt*speed+1e-8 {
		fraction = math.Min(1, dt*speed/distance)
	}
	x, y := old.X+dx*fraction/float64(cw), old.Y+dy*fraction/float64(ch)
	if dx < 0 {
		direction = 1
	} else if dx > 0 {
		direction = 0
	}
	next := old
	next.X, next.Y, next.T, next.Speed, next.Direction, next.Pond, next.Agent = x, y, in.KoiTime, speed, direction, lane, agent
	if old.Entering && math.Abs(x-right) < 1e-8 && math.Abs(y-float64(lane.Y)) < 1e-8 {
		next.Entering = false
	}
	if in.KoiMotion != nil {
		in.KoiMotion[agent.ID] = next
	}
	depth := koiHash(agent.Seed + 17)
	if agent.State == "idle" || agent.State == "done" {
		depth++
	}
	if agent.State == "blocked" {
		depth = 0
	}
	return KoiFish{Agent: agent, Rect: layout.Rect{X: int(x), Y: int(y), W: w, H: h}, Lane: lane, X: x, Y: y, Direction: direction, Wag: int(in.KoiTime*6) % 8, Depth: depth, Swimming: true}
}
