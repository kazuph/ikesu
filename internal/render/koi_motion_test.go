package render

import (
	"math"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
)

func TestKoiMotionIdleAndDoneKeepSwimming(t *testing.T) {
	for _, state := range []string{"idle", "done"} {
		t.Run(state, func(t *testing.T) {
			in := Input{KoiCellW: 8, KoiCellH: 16, KoiMotion: map[string]koiMotion{}}
			pond := layout.Rect{X: 130, Y: 2, W: 29, H: 60}
			agent := KoiAgent{ID: "local:1", Seed: 1, State: state}
			first := koiPondPose(agent, pond, in)
			last := first
			for tick := 1; tick <= 400; tick++ {
				in.KoiTime = float64(tick) * KoiInterval.Seconds()
				last = koiPondPose(agent, pond, in)
				if !last.Swimming || in.KoiMotion[agent.ID].Speed <= 0 {
					t.Fatal("fish stopped", tick)
				}
			}
			if math.Hypot(last.X-first.X, last.Y-first.Y) == 0 {
				t.Fatal("fish never moved")
			}
			if (KoiScene{Fish: []KoiFish{last}}).UpdateInterval() != KoiInterval {
				t.Fatal("lost 25ms clock")
			}
		})
	}
}

func TestKoiMotionResizeRetainsPositionAndSpeedLimit(t *testing.T) {
	in := Input{KoiCellW: 8, KoiCellH: 16, KoiMotion: map[string]koiMotion{}}
	agent := KoiAgent{ID: "local:1", Seed: 1, State: "working"}
	pond := layout.Rect{X: 130, Y: 2, W: 29, H: 60}
	old := koiPondPose(agent, pond, in)
	in.KoiTime = KoiInterval.Seconds()
	pond.H--
	next := koiPondPose(agent, pond, in)
	dots := math.Hypot((next.X-old.X)*8, (next.Y-old.Y)*16) / 2
	if dots > 1+1e-8 {
		t.Fatal("resize teleported fish", dots)
	}
}

func TestKoiMotionWholeSidebarAndTextRemain(t *testing.T) {
	in := koiFixture(t, 60)
	in.KoiKitty = true
	in.KoiCellW, in.KoiCellH = 8, 16
	plainIn := in
	plainIn.Koi = false
	plain := Compose(plainIn).Frame
	frame := Compose(in).Frame
	area := in.State.RightSidebarArea()
	if frame.KoiScene.Pond.Y != area.Y || frame.KoiScene.Pond.H != area.H {
		t.Fatal("list still shrinks pond", frame.KoiScene.Pond, area)
	}
	if len(frame.KoiScene.Fish) != KoiCount(60, area.W-1, area.H) {
		t.Fatal("population shrank with list")
	}
	for i, c := range plain.Cells {
		if c.Content != frame.Cells[i].Content {
			t.Fatal("list text changed", i)
		}
	}
	for _, g := range frame.Graphics {
		if g.Koi && g.Placement.Z >= 0 {
			t.Fatal("fish covers text", g.Placement.Z)
		}
	}
	for _, row := range SidebarViewForInput(in).Rows {
		if row.Right && row.Y >= frame.KoiScene.Pond.Y && row.Y < frame.KoiScene.ClickPond.Y {
			var clock KoiAnimation
			if clock.Click(frame.KoiScene, true, area.X+2, row.Y, time.Now()) {
				t.Fatal("fish stole list click")
			}
		}
	}
}

func TestKoiMotionMiniOnlyOnBands(t *testing.T) {
	in := koiFixture(t, 1)
	in.State.Workspaces[0].Section = model.SectionWork
	in.Panes[1].Status.Key = "working"
	frame := Compose(in).Frame
	if len(frame.KoiScene.Mini) == 0 {
		t.Fatal("working workspace has no band fish")
	}
	for _, fish := range frame.KoiScene.Mini {
		found := false
		for _, row := range SidebarViewForInput(in).Rows {
			if !row.Right && row.Y == fish.Rect.Y && row.Row.Kind == RowSection {
				found = true
			}
		}
		if !found {
			t.Fatal("mini on workspace row", fish.Rect)
		}
	}
}

func TestKoiMotionResidentsWaitEnterAndExit(t *testing.T) {
	in := Input{KoiCellW: 8, KoiCellH: 16, KoiMotion: map[string]koiMotion{}}
	pond := layout.Rect{X: 130, Y: 2, W: 29, H: 60}
	first := KoiAgent{ID: "local:2", Seed: 2, State: "working"}
	newcomer := KoiAgent{ID: "local:1", Seed: 1, State: "working"}
	scene := koiResidents([]KoiAgent{first}, 1, pond, in)
	if scene[0].X != float64(pond.X+pond.W) {
		t.Fatal("new fish appeared inside pond")
	}
	for tick := 1; tick <= 120; tick++ {
		in.KoiTime = float64(tick) * KoiInterval.Seconds()
		scene = koiResidents([]KoiAgent{newcomer, first}, 1, pond, in)
		if len(scene) != 1 || scene[0].Agent.ID != first.ID {
			t.Fatal("newcomer replaced resident", tick)
		}
	}
	if in.KoiMotion[first.ID].Entering {
		t.Fatal("entry never completed")
	}
	old := scene[0]
	in.KoiTime += KoiInterval.Seconds()
	scene = koiResidents([]KoiAgent{newcomer}, 1, pond, in)
	if scene[0].Agent.ID != first.ID || !in.KoiMotion[first.ID].Leaving {
		t.Fatal("removed fish vanished instead of exiting")
	}
	if math.Hypot(scene[0].X-old.X, scene[0].Y-old.Y) > 1 {
		t.Fatal("departure teleported")
	}
	for tick := 0; tick < 2000 && scene[0].Agent.ID == first.ID; tick++ {
		in.KoiTime += KoiInterval.Seconds()
		scene = koiResidents([]KoiAgent{newcomer}, 1, pond, in)
	}
	if scene[0].Agent.ID != newcomer.ID {
		t.Fatal("waiting newcomer never admitted")
	}
	if _, ok := in.KoiMotion[first.ID]; ok {
		t.Fatal("departed motion retained")
	}
}

func TestKoiMotionBlockedEvictsRestingThenNearest(t *testing.T) {
	in := Input{KoiCellW: 8, KoiCellH: 16, KoiMotion: map[string]koiMotion{}}
	pond := layout.Rect{X: 130, Y: 2, W: 29, H: 60}
	agents := []KoiAgent{{ID: "working", State: "working"}, {ID: "idle-far", State: "idle"}, {ID: "done-near", State: "done"}}
	koiResidents(agents, 3, pond, in)
	for _, agent := range agents {
		m := in.KoiMotion[agent.ID]
		m.Entering = false
		m.X = 140
		m.Y = 30
		if agent.ID == "working" || agent.ID == "done-near" {
			m.X = 130
		}
		in.KoiMotion[agent.ID] = m
	}
	blocked := KoiAgent{ID: "blocked", State: "blocked"}
	in.KoiTime = KoiInterval.Seconds()
	scene := koiResidents(append([]KoiAgent{blocked}, agents...), 3, pond, in)
	if len(scene) != 4 || !in.KoiMotion["done-near"].Leaving || in.KoiMotion["working"].Leaving || in.KoiMotion["idle-far"].Leaving {
		t.Fatal("wrong priority victim", in.KoiMotion)
	}
	if _, ok := in.KoiMotion[blocked.ID]; !ok {
		t.Fatal("blocked fish missing")
	}
	for tick := 0; tick < 200; tick++ {
		in.KoiTime += KoiInterval.Seconds()
		scene = koiResidents(append([]KoiAgent{blocked}, agents...), 3, pond, in)
	}
	if len(scene) != 3 {
		t.Fatal("temporary overflow did not drain", len(scene))
	}
}

func TestKoiMotionMiniLifecycleAndNamespace(t *testing.T) {
	in := koiFixture(t, 1)
	in.State.Workspaces[0].Section = model.SectionWork
	in.Panes[1].Status.Key = "working"
	in.KoiCellW, in.KoiCellH = 8, 16
	in.KoiKitty = true
	var clock KoiAnimation
	now := time.Unix(100, 0)
	clock.Apply(&in, nil, now, 15*time.Second)
	frame := Compose(in).Frame
	if len(frame.KoiScene.Mini) != 1 {
		t.Fatal("mini missing")
	}
	mini := frame.KoiScene.Mini[0]
	if mini.X != float64(mini.Lane.X+mini.Lane.W) {
		t.Fatal("mini did not enter at edge")
	}
	for tick := 1; tick <= 120; tick++ {
		clock.Apply(&in, nil, now.Add(time.Duration(tick)*KoiInterval), 15*time.Second)
		frame = AnimateKoi(frame.KoiScene, in)
	}
	if len(frame.KoiScene.Fish) != 1 || len(frame.KoiScene.Mini) != 1 {
		t.Fatal("pond and band identities mixed")
	}
	if clock.motion[mini.Agent.ID].Entering {
		t.Fatal("mini did not enter")
	}
	in.Panes[1].Status.Key = "idle"
	frame = Compose(in).Frame
	if len(frame.KoiScene.Mini) != 1 || !clock.motion[mini.Agent.ID].Leaving {
		t.Fatal("mini vanished on idle")
	}
	for tick := 121; tick <= 400; tick++ {
		clock.Apply(&in, nil, now.Add(time.Duration(tick)*KoiInterval), 15*time.Second)
		frame = AnimateKoi(frame.KoiScene, in)
	}
	if len(frame.KoiScene.Mini) != 0 {
		t.Fatal("departed mini retained")
	}
}

func TestKoiMotionMachineHeaderAndSectionBandCounts(t *testing.T) {
	in := koiFixture(t, 1)
	in.State.Workspaces[0].Section = model.SectionWork
	in.Panes[1].Status.Key = "working"
	remote := in
	machines := MachineSidebar{Groups: []MachineGroup{{ID: "local", Label: "local", Input: &in}, {ID: "remote", Label: "mini", Input: &remote}}, Active: "local"}
	frame := ComposeMachines(in, &machines).Frame
	headerFish, sectionFish := 0, 0
	for _, fish := range frame.KoiScene.Mini {
		found := false
		for _, hit := range machines.Hits {
			if hit.Right || hit.New || hit.Rect.Y != fish.Rect.Y {
				continue
			}
			if hit.Header {
				headerFish++
				found = true
				break
			}
			if hit.Row.Kind == RowSection && hit.Row.Section != model.SectionNone {
				sectionFish++
				found = true
				break
			}
		}
		if !found {
			t.Fatal("mini outside machine or colored section band", fish)
		}
	}
	if headerFish != 1 || sectionFish != 2 {
		t.Fatal("wrong number of working-workspace fish per band", headerFish, sectionFish)
	}
}

func TestKoiMotionBandCountUsesWorkspacesAndFitsWidth(t *testing.T) {
	in := koiFixture(t, 1)
	in.State.Workspaces[0].Section = model.SectionFavorite
	for i := 0; i < 4; i++ {
		in.State.Apply(model.CmdNewWorkspace{Section: model.SectionFavorite, NoFocus: true})
	}
	for _, ws := range in.State.Workspaces {
		if ws.SidebarSection() != model.SectionFavorite {
			continue
		}
		for _, tab := range ws.Tabs {
			for _, pid := range tab.Layout.PaneIDs() {
				in.Panes[pid] = &PaneSnap{Agent: "codex", Status: StatusGlyph{Key: "working"}}
			}
		}
	}
	frame := Compose(in).Frame
	favoriteBands := 0
	for _, band := range frame.KoiScene.Bands {
		if band.ID != "mini:local:favorite" {
			if len(band.Agents) != 0 {
				t.Fatal("idle default band unexpectedly contains working spaces")
			}
			continue
		}
		favoriteBands++
		if len(band.Agents) != 5 {
			t.Fatal("agents rather than working workspaces counted", len(band.Agents))
		}
		if band.Capacity != (in.State.SidebarArea().W-2)/8 {
			t.Fatal("band capacity not derived from width")
		}
		if len(frame.KoiScene.Mini) != min(5, band.Capacity) {
			t.Fatal("band overfilled", len(frame.KoiScene.Mini), band.Capacity)
		}
	}
	if favoriteBands != 1 {
		t.Fatal("favorite band missing or duplicated", favoriteBands)
	}
}

func TestKoiMotionHiddenSidebarsDoNotDrawDepartingMini(t *testing.T) {
	in := koiFixture(t, 1)
	in.State.Workspaces[0].Section = model.SectionWork
	in.Panes[1].Status.Key = "working"
	in.KoiKitty = true
	in.KoiCellW, in.KoiCellH = 8, 16
	var clock KoiAnimation
	now := time.Unix(100, 0)
	clock.Apply(&in, nil, now, 15*time.Second)
	Compose(in)
	clock.Apply(&in, nil, now.Add(KoiInterval), 15*time.Second)
	if len(Compose(in).Frame.KoiScene.Mini) == 0 {
		t.Fatal("fixture lacks live mini")
	}
	in.State.SidebarVisible = false
	in.State.SidebarHidden = true
	in.State.RightSidebarMode = "hidden"
	frame := Compose(in).Frame
	if len(frame.Graphics) != 0 || frame.KoiActive {
		t.Fatal("hidden sidebars retained koi output")
	}
}

func TestKoiMotionDefaultSpacesBandCountsWorkingWorkspaces(t *testing.T) {
	in := koiFixture(t, 1)
	// The default unclassified band is a real section, just like Work.
	for i := 0; i < 2; i++ {
		in.State.Apply(model.CmdNewWorkspace{Section: model.SectionNone, NoFocus: true})
	}
	working := 0
	for _, ws := range in.State.Workspaces {
		if ws.SidebarSection() != model.SectionNone {
			continue
		}
		if working == 2 {
			break
		}
		working++
		for _, tab := range ws.Tabs {
			for _, pid := range tab.Layout.PaneIDs() {
				in.Panes[pid] = &PaneSnap{Agent: "codex", Status: StatusGlyph{Key: "working"}}
			}
		}
	}
	frame := Compose(in).Frame
	for _, band := range frame.KoiScene.Bands {
		if band.ID != "mini:local:none" {
			continue
		}
		if len(band.Agents) != 2 {
			t.Fatal("default spaces band lost working workspaces", len(band.Agents))
		}
		if len(frame.KoiScene.Mini) != min(2, band.Capacity) {
			t.Fatal("default spaces band has no fish", len(frame.KoiScene.Mini))
		}
		return
	}
	t.Fatal("default spaces band excluded")
}
