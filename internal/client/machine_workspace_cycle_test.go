package client

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"reflect"
	"testing"
)

func cycleFixture(t *testing.T) *machineClient {
	t.Helper()
	c, err := config.Decode([]byte("[keys]\nnext_workspace='cmd+j'\nprevious_workspace='cmd+k'\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := &machineClient{active: "local", order: []string{"local", "mini"}, regions: &render.Output{}, endpoints: map[string]*managedEndpoint{}}
	peer := endpoint.Current()
	for _, id := range m.order {
		state := model.New(t.TempDir(), 120, 40)
		state.Apply(model.CmdNewWorkspace{})
		m.endpoints[id] = &managedEndpoint{profile: machine.Profile{ID: id, Enabled: true}, snapshot: &render.Input{State: state, SidebarConfig: c}, connection: &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}}
	}
	return m
}
func cycleKey(code rune) input.Event {
	return input.Event{Kind: input.KindKey, Key: uv.Key{Code: code, Mod: uv.ModSuper}, Raw: []byte("navigation")}
}

func TestWorkspaceCycleBoundariesAndWrap(t *testing.T) {
	for _, tc := range []struct {
		name, active string
		index        int
		key          rune
		target       string
		targetIndex  int
	}{
		{"next machine", "local", 1, 'j', "mini", 0}, {"next wraps", "mini", 1, 'j', "local", 0}, {"previous machine", "mini", 0, 'k', "local", 1}, {"previous wraps", "local", 0, 'k', "mini", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := cycleFixture(t)
			m.active = tc.active
			m.endpoints[tc.active].snapshot.State.Active = tc.index
			if !m.workspaceCycleInput(cycleKey(tc.key)) {
				t.Fatal("not intercepted")
			}
			if m.pending == nil || m.pending.id != tc.target || m.pending.workspace != m.endpoints[tc.target].snapshot.State.Workspaces[tc.targetIndex].ID {
				t.Fatalf("pending=%+v", m.pending)
			}
			if m.active != tc.active {
				t.Fatal("switched without snapshot confirmation")
			}
		})
	}
}
func TestWorkspaceCycleOrderSkipsUnavailableAndHidden(t *testing.T) {
	m := cycleFixture(t)
	s := m.endpoints["local"].snapshot.State
	s.Apply(model.CmdNewWorkspace{})
	s.Apply(model.CmdSetSection{Index: 0, Section: model.SectionFavorite})
	s.Apply(model.CmdSetSection{Index: 1, Section: model.SectionPersonal})
	s.Apply(model.CmdToggleSection{Section: model.SectionPersonal})
	m.order = []string{"mini", "disabled", "offline", "missing", "empty", "local"}
	m.endpoints["disabled"] = &managedEndpoint{profile: machine.Profile{Enabled: false}, snapshot: m.endpoints["mini"].snapshot, connection: m.endpoints["mini"].connection}
	m.endpoints["offline"] = &managedEndpoint{profile: machine.Profile{Enabled: true}, snapshot: m.endpoints["mini"].snapshot}
	m.endpoints["missing"] = &managedEndpoint{profile: machine.Profile{Enabled: true}, connection: m.endpoints["mini"].connection}
	m.endpoints["empty"] = &managedEndpoint{profile: machine.Profile{Enabled: true}, snapshot: &render.Input{State: &model.AppState{}}, connection: m.endpoints["mini"].connection}
	want := []workspaceCycleTarget{{"mini", m.endpoints["mini"].snapshot.State.Workspaces[0].ID}, {"mini", m.endpoints["mini"].snapshot.State.Workspaces[1].ID}, {"local", s.Workspaces[0].ID}, {"local", s.Workspaces[2].ID}}
	if got := m.workspaceCycleTargets(); !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v want %+v", got, want)
	}
	// No screen hit is needed, including when every row is off screen.
	if len(m.sidebar.Hits) != 0 {
		t.Fatal("fixture has hits")
	}
}
func TestWorkspaceCyclePrecedenceAndLegacy(t *testing.T) {
	for _, name := range []string{"dialog", "menu", "help", "navigator", "settings", "prefix", "copy", "mode", "local command", "remote command", "unbound", "one machine", "number key", "unmatched release"} {
		t.Run(name, func(t *testing.T) {
			m := cycleFixture(t)
			m.endpoints["local"].snapshot.State.Active = 1
			v := m.endpoints["local"].snapshot
			event := cycleKey('j')
			switch name {
			case "dialog":
				v.Dialog = &render.Dialog{}
			case "menu":
				v.Menu = &render.Menu{}
			case "help":
				v.Help = &render.HelpInput{}
			case "navigator":
				v.Navigator = &render.Navigator{}
			case "settings":
				v.Settings = &render.Settings{}
			case "prefix":
				v.Prefix = true
			case "copy":
				v.Copy = &render.CopyMark{}
			case "mode":
				v.ModeHint = "RESIZE"
			case "local command":
				v.SidebarConfig.Keys.Command = append(v.SidebarConfig.Keys.Command, config.PluginKeyBinding{Key: "cmd+j"})
			case "remote command":
				m.active = "mini"
				m.endpoints["mini"].snapshot.SidebarConfig.Keys.Command = append(v.SidebarConfig.Keys.Command, config.PluginKeyBinding{Key: "cmd+j"})
			case "unbound":
				v.SidebarConfig = config.Config{}
			case "one machine":
				m.order = []string{"local"}
			case "same machine":
				v.State.Active = 0
			case "number key":
				event = cycleKey('1')
			case "unmatched release":
				event.Kind = input.KindKeyRelease
			}
			if m.workspaceCycleInput(event) || m.pending != nil {
				t.Fatal("stole server input")
			}
		})
	}
}
func TestWorkspaceCyclePendingRepeatReleaseTimeout(t *testing.T) {
	m := cycleFixture(t)
	m.endpoints["local"].snapshot.State.Active = 1
	if err := m.input(cycleKey('j')); err != nil {
		t.Fatal(err)
	}
	pending := m.pending
	for i := 0; i < 3; i++ {
		for _, k := range []rune{'j', 'k'} {
			if err := m.input(cycleKey(k)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if m.pending == nil || len(m.pending.keys) != 0 {
		t.Fatal("navigation queued as pane input")
	}
	for _, k := range []rune{'j', 'k'} {
		e := cycleKey(k)
		e.Kind = input.KindKeyRelease
		if !m.workspaceCycleInput(e) {
			t.Fatal("release leaked")
		}
	}
	m.advance(pending.deadline)
	if m.pending != nil || m.active != "local" {
		t.Fatal("timeout changed display")
	}
	for _, e := range m.endpoints {
		for _, msg := range e.connection.queue {
			if msg.Type == "input" {
				t.Fatal("navigation leaked to pane")
			}
		}
	}
}

func TestWorkspaceCycleLocalBindingsOnRemoteAndReleaseAfterModeChange(t *testing.T) {
	m := cycleFixture(t)
	m.active = "mini"
	m.endpoints["mini"].snapshot.SidebarConfig = config.Config{}
	m.endpoints["mini"].snapshot.State.Active = 0
	if !m.workspaceCycleInput(cycleKey('j')) || m.pending == nil || m.pending.id != "mini" || m.pending.workspace != m.endpoints["mini"].snapshot.State.Workspaces[1].ID {
		t.Fatalf("local binding not used: %+v", m.pending)
	}
	m.endpoints["mini"].snapshot.Prefix = true
	event := cycleKey('j')
	event.Kind = input.KindKeyRelease
	if !m.workspaceCycleInput(event) {
		t.Fatal("release leaked after snapshot changed")
	}
}

func TestWorkspaceCycleCollapsedWorktreeGroup(t *testing.T) {
	m := cycleFixture(t)
	s := m.endpoints["local"].snapshot.State
	s.Active = 0
	s.Workspaces[0].Worktree = &api.WorkspaceWorktreeInfo{RepoKey: "repo"}
	s.Workspaces[0].GroupCollapsed = true
	s.Workspaces[1].Worktree = &api.WorkspaceWorktreeInfo{RepoKey: "repo", IsLinkedWorktree: true}
	targets := m.workspaceCycleTargets()
	if len(targets) != 3 || targets[0].workspace != s.Workspaces[0].ID || targets[1].machine != "mini" {
		t.Fatalf("hidden child included: %+v", targets)
	}
	s.Active = 1
	if got := m.workspaceCycleTargets(); len(got) != 4 {
		t.Fatalf("active child hidden: %+v", got)
	}
}

func TestWorkspaceCycleReleaseTracksPhysicalKeyAndBlur(t *testing.T) {
	for _, mod := range []uv.KeyMod{0, uv.ModSuper | uv.ModShift} {
		m := cycleFixture(t)
		m.endpoints["local"].snapshot.State.Active = 1
		if !m.workspaceCycleInput(cycleKey('j')) {
			t.Fatal("press not consumed")
		}
		release := cycleKey('j')
		release.Kind = input.KindKeyRelease
		release.Key.Mod = mod
		if !m.workspaceCycleInput(release) {
			t.Fatal("physical release leaked", mod)
		}
	}
	m := cycleFixture(t)
	m.endpoints["local"].snapshot.State.Active = 1
	m.workspaceCycleInput(cycleKey('j'))
	m.workspaceCycleInput(input.Event{Kind: input.KindBlur})
	release := cycleKey('j')
	release.Kind = input.KindKeyRelease
	if m.workspaceCycleInput(release) {
		t.Fatal("blur retained old press")
	}
	m.pending = nil
	if !m.workspaceCycleInput(cycleKey('j')) || !m.workspaceCycleInput(release) {
		t.Fatal("new press/release lost after blur")
	}
}

func TestWorkspaceCycleBatchAndRepeatCrossBothBoundaries(t *testing.T) {
	for _, dir := range []rune{'j', 'k'} {
		for _, repeat := range []bool{false, true} {
			m := cycleFixture(t)
			start, target := 0, 0
			if dir == 'k' {
				start, target = 1, 1
			}
			m.endpoints["local"].snapshot.State.Active = start
			first := cycleKey(dir)
			second := first
			second.Key.IsRepeat = repeat
			if err := m.input(first); err != nil {
				t.Fatal(err)
			}
			if err := m.input(second); err != nil {
				t.Fatal(err)
			}
			if m.pending == nil || m.pending.id != "mini" || m.pending.workspace != m.endpoints["mini"].snapshot.State.Workspaces[target].ID {
				t.Fatalf("key=%c repeat=%t pending=%+v", dir, repeat, m.pending)
			}
		}
	}
}

func TestWorkspaceCycleOutstandingInputAndPrefixPriority(t *testing.T) {
	for _, raw := range []string{"\x02", "\x02W"} {
		m := cycleFixture(t)
		m.endpoints["local"].snapshot.State.Active = 1
		if err := m.sendInput("local", []byte(raw)); err != nil {
			t.Fatal(err)
		}
		if err := m.input(cycleKey('j')); err != nil {
			t.Fatal(err)
		}
		e := m.endpoints["local"]
		if m.pending != nil || len(e.connection.queue) != 2 || e.connection.queue[0].InputSequence != 1 || e.connection.queue[1].InputSequence != 2 {
			t.Fatal("stale snapshot stole ordered input", m.pending, e.connection.queue)
		}
		e.processedInput = 1
		if m.workspaceCycleInput(cycleKey('j')) {
			t.Fatal("partial receipt unlocked navigation")
		}
		e.processedInput = 2
		if !m.workspaceCycleInput(cycleKey('j')) {
			t.Fatal("fully received input kept navigation blocked")
		}
	}
	m := cycleFixture(t)
	m.endpoints["local"].snapshot.State.Active = 1
	m.active = "mini"
	m.endpoints["mini"].snapshot.SidebarConfig.Keys.Prefix = "cmd+j"
	if m.workspaceCycleInput(cycleKey('j')) {
		t.Fatal("cycle overrode prefix key")
	}
}

func TestWorkspaceCycleOldGenerationForwardsKeys(t *testing.T) {
	for _, active := range []string{"local", "mini"} {
		m := cycleFixture(t)
		m.active = active
		e := m.endpoints[active]
		e.snapshot.State.Active = 1
		e.connection.writer.available = endpoint.InputReceiptGeneration - 1
		if err := m.input(cycleKey('j')); err != nil {
			t.Fatal(err)
		}
		if m.pending != nil || len(e.connection.queue) != 1 || e.connection.queue[0].Type != "input" || e.connection.queue[0].InputSequence != 0 {
			t.Fatal("old endpoint lost ordinary navigation")
		}
	}
}

func TestWorkspaceCycleReceiptOrderingWithRealServers(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	cfg, err := config.Decode([]byte("onboarding=false\n[keys]\nnext_workspace='cmd+j'\nprevious_workspace='cmd+k'\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.endpoints {
		if err := e.connection.enqueue(msg{Type: "view_config", Config: &cfg}); err != nil {
			t.Fatal(err)
		}
	}
	h.until("navigation config", func() bool {
		for _, e := range m.endpoints {
			if workspaceCycleDirection(e.snapshot.SidebarConfig, cycleKey('j').Key) != 1 {
				return false
			}
		}
		return true
	})
	e := m.endpoints["local"]
	if err := e.connection.enqueue(msg{Type: "workspace_select", Workspace: e.snapshot.State.Workspaces[1].ID}); err != nil {
		t.Fatal(err)
	}
	h.until("local last acknowledged", func() bool { return e.snapshot.State.Active == 1 && e.processedInput == e.connection.lastSentInput() })
	for _, raw := range []string{"\x02", "\x02W"} {
		if err := m.sendInput("local", []byte(raw)); err != nil {
			t.Fatal(err)
		}
		if err := m.input(cycleKey('j')); err != nil {
			t.Fatal(err)
		}
		if m.pending != nil {
			t.Fatal("stole key before prefix/dialog receipt")
		}
		h.until("ordered input receipt", func() bool { return e.processedInput == e.connection.lastSentInput() })
		if e.snapshot.State.Active != 1 {
			t.Fatal("prefix/dialog input moved workspace")
		}
		if err := m.sendInput("local", []byte("\x1b[27u")); err != nil {
			t.Fatal(err)
		}
		h.until("overlay closed and acknowledged", func() bool {
			return e.snapshot.Dialog == nil && !e.snapshot.Prefix && e.processedInput == e.connection.lastSentInput()
		})
	}
	if err := m.input(cycleKey('j')); err != nil {
		t.Fatal(err)
	}
	h.until("cross machine after receipt", func() bool {
		return m.pending == nil && m.active == "remote" && m.endpoints["remote"].snapshot.State.Active == 0
	})
}
