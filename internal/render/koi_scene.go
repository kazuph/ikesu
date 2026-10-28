package render

import (
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
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
	Lane           layout.Rect
	X, Y           float64
	Direction, Wag int
	Depth          float64
	Swimming       bool
}
type koiBand struct {
	ID       string
	Lane     layout.Rect
	Agents   []KoiAgent
	Capacity int
}

type KoiScene struct {
	Bands     []koiBand
	Agents    []KoiAgent
	Capacity  int
	Pond      layout.Rect
	ClickPond layout.Rect
	Fish      []KoiFish
	Mini      []KoiFish
	Ripple    *KoiRipple
	Active    bool
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
	scene.ClickPond = layout.Rect{X: a.X + 1, Y: last, W: max(0, a.W-1), H: max(0, a.Y+a.H-last)}
	scene.Pond = layout.Rect{X: a.X + 1, Y: a.Y, W: max(0, a.W-1), H: a.H}
	agents := KoiAgentsFor(in, machines)
	for i := range agents {
		agents[i].Glow = in.KoiGlow[agents[i].ID]
	}
	n := KoiCount(len(agents), scene.Pond.W, scene.Pond.H)
	if in.JobsPanel || a.W <= 1 {
		n = 0
	}
	scene.Agents, scene.Capacity = agents, n
	if !in.JobsPanel && a.W > 1 {
		scene.Fish = koiResidents(agents, n, scene.Pond, in)
	}
	sort.SliceStable(scene.Fish, func(i, j int) bool { return scene.Fish[i].Depth > scene.Fish[j].Depth })
	scene.Ripple = in.KoiRipple
	for _, fish := range scene.Fish {
		scene.Active = scene.Active || fish.Swimming || fish.Agent.State == "blocked" || fish.Agent.Glow
	}
	if in.KoiRipple != nil {
		scene.Active = true
	}
	left := s.SidebarArea()
	if left.W > 1 && !in.Dragging && in.Copied == "" {
		band := func(src Input, key string, row SidebarRow, header bool, y int) {
			if !header && (row.Kind != RowSection || row.Section == model.SectionNone) {
				return
			}
			start, width := left.X+1, left.W-2
			if width < 8 {
				return
			}
			band := koiBand{ID: key, Lane: layout.Rect{X: start, Y: y, W: width, H: 1}, Capacity: width / 8}
			for wi, ws := range src.State.Workspaces {
				if !header && ws.SidebarSection() != row.Section {
					continue
				}
				if src.State.WorktreeHidden(wi) {
					continue
				}
				working := false
				for _, tab := range ws.Tabs {
					for _, pid := range tab.Layout.PaneIDs() {
						if p := src.Panes[pid]; p != nil && p.Agent != "" && p.Status.Key == "working" {
							working = true
						}
					}
				}
				if !working {
					continue
				}
				agent := KoiAgent{ID: key + ":" + itoa(ws.ID), State: "working", Seed: ws.ID}
				band.Agents = append(band.Agents, agent)
			}
			scene.Bands = append(scene.Bands, band)
		}
		if machines != nil && len(machines.Groups) > 1 {
			for _, hit := range machines.Hits {
				if hit.Right || hit.New {
					continue
				}
				for _, g := range machines.Groups {
					if g.ID == hit.Machine && g.Input != nil {
						band(*g.Input, "mini:"+g.ID+":"+string(hit.Row.Section), hit.Row, hit.Header, hit.Rect.Y)
					}
				}
			}
		} else {
			for _, p := range view.Rows {
				if !p.Right {
					band(in, "mini:local:"+string(p.Row.Section), p.Row, false, p.Y)
				}
			}
		}
	}
	seen := map[string]bool{}
	for _, band := range scene.Bands {
		seen[band.ID] = true
	}
	for _, m := range in.KoiMotion {
		if m.Group != "" && !seen[m.Group] {
			scene.Bands = append(scene.Bands, koiBand{ID: m.Group, Lane: m.Pond})
			seen[m.Group] = true
		}
	}
	for _, band := range scene.Bands {
		scene.Mini = append(scene.Mini, koiResidentsFor(band.Agents, band.Capacity, band.Lane, in, band.ID, 6, 1)...)
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
	area := lane
	if lane.H > 1 {
		area.H++
	}
	return KoiFish{Agent: agent, Rect: layout.Rect{X: int(x), Y: lane.Y, W: width, H: lane.H}, Lane: area, X: x, Y: y, Direction: direction, Wag: wag}
}

func itoa(n int) string { // IDs only, not user-facing labels.
	return strconv.Itoa(n)
}

func drawKoiDecoration(f *Frame, in Input, machines *MachineSidebar) {
	scene := KoiSceneFor(f, in, machines)
	f.KoiScene = scene
	f.KoiActive = scene.Active || len(scene.Mini) > 0
	if in.KoiKitty {
		drawKoiGraphics(f, scene, in)
		return
	}
	drawKoiRippleOctants(f, scene)
	for _, fish := range append(scene.Fish, scene.Mini...) {
		drawKoiOctants(f, fish)
	}
}
