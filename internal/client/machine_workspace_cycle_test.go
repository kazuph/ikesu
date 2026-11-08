package client

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOyagoiCycleUsesNormalWorkspaceInOtherSessionReview(t *testing.T) {
	for _, tc := range []struct {
		name   string
		key    rune
		active int
	}{{"next", 'j', 1}, {"previous", 'k', 0}} {
		t.Run(tc.name, func(t *testing.T) {
			m := cycleFixture(t)
			m.session = "local"
			remote := m.endpoints["mini"]
			remote.profile.Session = "session-B"
			remote.snapshot.State.Workspaces = remote.snapshot.State.Workspaces[:1]
			ws := remote.snapshot.State.Workspaces[0]
			selection := oyagoiSelection{Machine: "mini", Session: "session-A", Workspace: ws.ID, Generation: "old-generation"}
			m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
			if err := m.oyagoiStore.set(&selection); err != nil {
				t.Fatal(err)
			}
			m.oyagoi = &selection
			m.endpoints["local"].snapshot.State.Active = tc.active
			if !m.oyagoiForSidebar().Offline {
				t.Fatal("old HQ must remain offline")
			}
			if !m.workspaceCycleInput(cycleKey(tc.key)) {
				t.Fatal("cycle key not handled")
			}
			if m.pending == nil || m.pending.id != "mini" || m.pending.workspace != ws.ID || m.pending.generation != "" {
				t.Fatalf("normal workspace was not selected: %+v, notice %q", m.pending, m.notice)
			}
			found := false
			for _, message := range remote.connection.queue {
				if message.Type == "workspace_select" && message.Workspace == ws.ID && message.WorkspaceGeneration == "" {
					found = true
				}
			}
			if !found {
				t.Fatal("normal workspace selection was not sent")
			}
			got, err := m.oyagoiStore.load()
			if err != nil || got == nil || *got != selection {
				t.Fatalf("old HQ changed: %+v %v", got, err)
			}
		})
	}
}

func TestOyagoiCycleHQCarriesGenerationAndRejectsReplacementReview(t *testing.T) {
	m := cycleFixture(t)
	m.session = "local"
	remote := m.endpoints["mini"]
	remote.profile.Session = "session-A"
	ws := remote.snapshot.State.Workspaces[0]
	selected := oyagoiSelection{Machine: "mini", Session: "session-A", Workspace: ws.ID, Generation: ws.Generation}
	m.oyagoi = &selected
	m.endpoints["local"].snapshot.State.Active = 0
	m.size.Cols, m.size.Rows = 120, 40
	if !m.workspaceCycleInput(cycleKey('k')) {
		t.Fatal("cycle key not handled")
	}
	if m.pending == nil || m.pending.id != "mini" || m.pending.generation != selected.Generation {
		t.Fatalf("HQ identity missing: %+v", m.pending)
	}
	found := false
	for _, item := range remote.connection.queue {
		if item.Type == "workspace_select" && item.Workspace == ws.ID && item.WorkspaceGeneration == selected.Generation {
			found = true
		}
	}
	if !found {
		t.Fatal("HQ request lacks generation")
	}
	remote.processedInput = m.pending.inputSequence
	ws.Generation = "replacement"
	m.finishSwitch()
	if m.active != "local" || m.pending == nil {
		t.Fatal("replacement workspace was accepted")
	}
}

// Uses two live servers and their shell PTYs for the HQ selection.
func TestOyagoiCycleSameHQActualPTYReview(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
	local, remote := m.endpoints["local"], m.endpoints["remote"]
	ws := remote.snapshot.State.Workspaces[0]
	if err := m.oyagoiAction("remote", fmt.Sprintf("oyagoi:set:%d:%s", ws.ID, ws.Generation)); err != nil {
		t.Fatal(err)
	}
	h.until("HQ projected", func() bool { return remote.snapshot.MobileOyagoi == ws.ID })
	bindings, err := config.Decode([]byte("[keys]\nnext_workspace='cmd+j'\nprevious_workspace='cmd+k'\n"))
	if err != nil {
		t.Fatal(err)
	}
	local.snapshot.SidebarConfig, remote.snapshot.SidebarConfig = bindings, bindings
	local.snapshot.State.Active = 0
	if !m.workspaceCycleInput(cycleKey('k')) {
		t.Fatal("cycle key not handled")
	}
	if m.pending == nil || m.pending.generation != ws.Generation {
		t.Fatalf("HQ identity missing: %+v", m.pending)
	}
	h.until("same HQ selected", func() bool { return m.active == "remote" && m.pending == nil })
	if got := remote.snapshot.State.ActiveWorkspace(); got == nil || got.ID != ws.ID || got.Generation != ws.Generation {
		t.Fatalf("wrong HQ: %+v", got)
	}
}

// Two real servers and their shell PTYs can take more than five seconds to start.
func TestOyagoiCycleOtherSessionActualPTYReview(t *testing.T) {
	for _, tc := range []struct {
		name   string
		key    rune
		active int
	}{{"next", 'j', 1}, {"previous", 'k', 0}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRightMachineHarness(t)
			m := h.m
			local, remote := m.endpoints["local"], m.endpoints["remote"]
			old, current := local.snapshot.State.Workspaces[0], remote.snapshot.State.Workspaces[0]
			if old.ID != current.ID || old.Generation == current.Generation {
				t.Fatal("fixture requires same number with different generation")
			}
			m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
			selected := oyagoiSelection{Machine: "remote", Session: "right-local", Workspace: old.ID, Generation: old.Generation}
			if err := m.oyagoiStore.set(&selected); err != nil {
				t.Fatal(err)
			}
			if err := m.refreshOyagoi(); err != nil {
				t.Fatal(err)
			}
			h.api("remote", api.MethodWorkspaceClose, map[string]any{"workspace_id": "s2"})
			h.until("one remote workspace", func() bool { return len(remote.snapshot.State.Workspaces) == 1 })
			bindings, err := config.Decode([]byte("[keys]\nnext_workspace='cmd+j'\nprevious_workspace='cmd+k'\n"))
			if err != nil {
				t.Fatal(err)
			}
			local.snapshot.SidebarConfig, remote.snapshot.SidebarConfig = bindings, bindings
			local.snapshot.State.Active = tc.active
			if !m.oyagoiForSidebar().Offline {
				t.Fatal("old HQ must remain offline")
			}
			if !m.workspaceCycleInput(cycleKey(tc.key)) {
				t.Fatal("cycle key not handled")
			}
			h.until("normal workspace selected", func() bool { return m.active == "remote" && m.pending == nil })
			if got := remote.snapshot.State.ActiveWorkspace(); got == nil || got.ID != current.ID || got.Generation != current.Generation {
				t.Fatalf("wrong destination: %+v", got)
			}
			got, err := m.oyagoiStore.load()
			if err != nil || got == nil || *got != selected || !m.oyagoiForSidebar().Offline {
				t.Fatalf("old HQ changed: %+v %v", got, err)
			}
		})
	}
}

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
	want := []workspaceCycleTarget{{machine: "mini", workspace: m.endpoints["mini"].snapshot.State.Workspaces[0].ID}, {machine: "mini", workspace: m.endpoints["mini"].snapshot.State.Workspaces[1].ID}, {machine: "local", workspace: s.Workspaces[0].ID}, {machine: "local", workspace: s.Workspaces[2].ID}}
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
	if teststate.RunIsolatedParallel(t) {
		return
	}
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
