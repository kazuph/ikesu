package render

import (
	"github.com/kazuph/ikesu/internal/layout"
	"math"
	"sort"
	"strconv"
)

// KoiAgent IDs include the endpoint so identical pane IDs never alias.
type KoiAgent struct {
	ID, State string
	Seed      int
	Glow      bool
}
type KoiFish struct {
	Agent          KoiAgent
	Rect           layout.Rect
	X, Y           float64
	Direction, Wag int
}
type KoiScene struct {
	Pond layout.Rect
	Fish []KoiFish
	Mini []KoiFish
}

func KoiAgentsFor(in Input, machines *MachineSidebar) []KoiAgent {
	var agents []KoiAgent
	add := func(prefix string, src Input) {
		ids := make([]int, 0, len(src.Panes))
		for id, p := range src.Panes {
			if p != nil && p.Agent != "" {
				ids = append(ids, id)
			}
		}
		sort.Ints(ids)
		for _, id := range ids {
			p := src.Panes[id]
			agents = append(agents, KoiAgent{ID: prefix + ":" + itoa(id), State: p.Status.Key, Seed: id})
		}
	}
	if machines != nil && len(machines.Groups) > 1 {
		for _, g := range machines.Groups {
			if g.Input != nil {
				add(g.ID, *g.Input)
			}
		}
	} else {
		add("local", in)
	}
	// Stable priority preserves identities while ensuring a blocked fish survives thinning.
	sort.SliceStable(agents, func(i, j int) bool { return agents[i].State == "blocked" && agents[j].State != "blocked" })
	return agents
}

func KoiSceneFor(f *Frame, in Input, machines *MachineSidebar) KoiScene {
	var scene KoiScene
	if !in.Koi || !in.SidebarConfig.KoiEnabled() || in.Menu != nil || in.Dialog != nil || in.Help != nil || in.Settings != nil || in.Navigator != nil || in.State.Mobile() {
		return scene
	}
	s := in.State
	view := SidebarViewForInput(in)
	last := view.Agents.Rect.Y
	for _, p := range view.Rows {
		if p.Right {
			last = max(last, p.Y+1)
		}
	}
	if machines != nil && len(machines.Groups) > 1 {
		last = 2
		for _, hit := range machines.Hits {
			if hit.Right {
				last = max(last, hit.Rect.Y+hit.Rect.H)
			}
		}
	}
	a := s.RightSidebarArea()
	scene.Pond = layout.Rect{X: a.X + 1, Y: last, W: max(0, a.W-1), H: max(0, a.Y+a.H-last)}
	agents := KoiAgentsFor(in, machines)
	for i := range agents {
		agents[i].Glow = in.KoiGlow[agents[i].ID]
	}
	n := KoiCount(len(agents), scene.Pond.W, scene.Pond.H)
	if in.JobsPanel || a.W <= 1 {
		n = 0
	}
	for i := 0; i < n; i++ {
		lane := layout.Rect{X: scene.Pond.X, Y: scene.Pond.Y + i*3, W: scene.Pond.W, H: 2}
		scene.Fish = append(scene.Fish, koiPose(agents[i], lane, 10, in.KoiTime))
	}
	mini := func(src Input, row SidebarRow, y, x, end int) {
		if row.Kind != RowWorkspace || row.Ws < 0 || row.Ws >= len(src.State.Workspaces) {
			return
		}
		var working bool
		ws := src.State.Workspaces[row.Ws]
		for _, tab := range ws.Tabs {
			for _, pid := range tab.Layout.PaneIDs() {
				if p := src.Panes[pid]; p != nil && p.Agent != "" && p.Status.Key == "working" {
					working = true
				}
			}
		}
		if !working {
			return
		}
		start, w := blankRun(f, y, x, end)
		if w < 8 {
			return
		}
		scene.Mini = append(scene.Mini, koiPose(KoiAgent{ID: itoa(ws.ID), State: "working", Seed: ws.ID}, layout.Rect{X: start, Y: y, W: w, H: 1}, 6, in.KoiTime))
	}
	left := s.SidebarArea()
	if left.W > 1 && !in.Dragging && in.Copied == "" {
		if machines != nil && len(machines.Groups) > 1 {
			for _, hit := range machines.Hits {
				if hit.Right || hit.Header || hit.New {
					continue
				}
				for _, g := range machines.Groups {
					if g.ID == hit.Machine && g.Input != nil {
						mini(*g.Input, hit.Row, hit.Rect.Y, left.X, left.X+left.W-2)
					}
				}
			}
		} else {
			for _, p := range view.Rows {
				if !p.Right {
					mini(in, p.Row, p.Y, left.X, left.X+left.W-2)
				}
			}
		}
	}
	return scene
}

func koiPose(agent KoiAgent, lane layout.Rect, width int, t float64) KoiFish {
	phase := koiHash(agent.Seed+29) * 2 * math.Pi
	speed := 0.18 + koiHash(agent.Seed+11)*0.12
	if agent.State == "idle" || agent.State == "done" {
		speed *= 0.18
	}
	u := t*speed + phase
	x := float64(lane.X) + (math.Sin(u)*0.5+0.5)*float64(max(0, lane.W-width))
	direction := 0
	if math.Cos(u) < 0 {
		direction = 1
	}
	wag := int(math.Mod(t*6+phase*3, 2*math.Pi)/(2*math.Pi)*8) % 8
	if agent.State == "blocked" {
		x = float64(lane.X) + (float64(lane.W-width) / 2)
		wag = int(t*6) % 8
	}
	y := float64(lane.Y)
	if lane.H > 1 && (agent.State == "idle" || agent.State == "done") {
		y += 0.5
	}
	return KoiFish{Agent: agent, Rect: layout.Rect{X: int(x), Y: lane.Y, W: width, H: lane.H}, X: x, Y: y, Direction: direction, Wag: wag}
}

func itoa(n int) string { // IDs only, not user-facing labels.
	return strconv.Itoa(n)
}

func drawKoiDecoration(f *Frame, in Input, machines *MachineSidebar) {
	scene := KoiSceneFor(f, in, machines)
	f.KoiActive = len(scene.Fish)+len(scene.Mini) > 0
	if in.KoiKitty {
		drawKoiGraphics(f, scene, in)
		return
	}
	if len(scene.Fish) > 0 {
		for y := scene.Pond.Y; y < scene.Pond.Y+scene.Pond.H; y++ {
			for x := scene.Pond.X; x < scene.Pond.X+scene.Pond.W; x++ {
				if c := f.Cell(x, y); c != nil {
					style := c.Style
					style.Bg = waterBG
					f.Set(x, y, " ", style)
				}
			}
		}
	}
	for _, fish := range append(scene.Fish, scene.Mini...) {
		drawKoiOctants(f, fish)
	}
}
