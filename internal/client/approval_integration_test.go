package client

import (
	"context"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
)

func TestGuardApprovalThroughDisplayConnection(t *testing.T) {
	h := newRightMachineHarness(t)
	m, e := h.m, h.m.endpoints["local"]
	h.api("local", api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "command_guard", Title: "guard approval", Origin: &api.DecisionOrigin{PaneID: "p1", Command: "true"}, Options: []api.DecisionOption{{ID: "approve", Label: "approve", Role: "approve"}, {ID: "day_allow", Label: "day", Role: "approve"}, {ID: "reject", Label: "reject", Role: "reject"}}})
	h.until("approval opens", func() bool { return e.guardApproval.current() != nil && m.regions.ApprovalRect.W > 0 })
	d := e.guardApproval.current().DecisionID
	key := func(code rune) {
		t.Helper()
		if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: code}}); err != nil {
			t.Fatal(err)
		}
	}
	key('1')
	if e.guardApproval.open != d || e.guardApproval.waiting {
		t.Fatal("number answered before Enter")
	}
	key(uv.KeyEscape)
	if e.guardApproval.open != "" || len(e.guardApproval.pending) != 1 {
		t.Fatal("escape did not preserve pending decision")
	}
	// Prefix and command can be typed before the next server snapshot arrives.
	if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: 'b', Mod: uv.ModCtrl}, Raw: []byte{2}}); err != nil {
		t.Fatal(err)
	}
	key('a')
	if e.guardApproval.open != d {
		t.Fatal("prefix+a did not reopen")
	}
	key('1')
	key(uv.KeyEnter)
	h.until("answer propagated", func() bool { return len(e.guardApproval.pending) == 0 })
	if e.guardApproval.open != "" {
		t.Fatal("resolved approval remains open")
	}
}

func TestGuardApprovalTypingClickAndExternalResolution(t *testing.T) {
	h := newRightMachineHarness(t)
	m, e := h.m, h.m.endpoints["local"]
	if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: '1'}, Raw: []byte("1")}); err != nil {
		t.Fatal(err)
	}
	body := "test request"
	create := api.DecisionCreateParams{Kind: "command_guard", Title: "guard approval", Body: &body, Origin: &api.DecisionOrigin{PaneID: "p1", Command: "true"}, Options: []api.DecisionOption{{ID: "approve", Label: "approve", Role: "approve"}}}
	h.api("local", api.MethodDecisionCreate, create)
	h.until("indicator", func() bool { return len(e.guardApproval.pending) == 1 && m.regions.AnswerRects[1].W > 0 })
	if e.guardApproval.open != "" {
		t.Fatal("approval took focus during typing")
	}
	for _, event := range []input.Event{{Kind: input.KindKey, Key: uv.Key{Code: '1'}, Raw: []byte("1")}, {Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEnter}, Raw: []byte("\r")}} {
		if err := m.input(event); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.guardApproval.pending) != 1 || e.guardApproval.open != "" {
		t.Fatal("typed number and Enter acted on approval")
	}
	r := m.regions.AnswerRects[1]
	click := input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: r.X, Y: r.Y, Button: uv.MouseLeft}}
	if err := m.input(click); err != nil {
		t.Fatal(err)
	}
	if e.guardApproval.open == "" {
		t.Fatal("click did not open approval")
	}
	id := e.guardApproval.open
	h.api("local", api.MethodDecisionCancel, api.DecisionTargetParams{DecisionID: id})
	h.until("external cancel", func() bool { return len(e.guardApproval.pending) == 0 && e.guardApproval.open == "" })
	timeout := uint64(200)
	create.TimeoutMS = &timeout
	h.api("local", api.MethodDecisionCreate, create)
	h.until("expiry pending", func() bool { return len(e.guardApproval.pending) == 1 })
	h.until("expiry cleared", func() bool { return len(e.guardApproval.pending) == 0 && e.guardApproval.open == "" })
}

func TestGuardApprovalKeepsScratchPopupOpen(t *testing.T) {
	h := newRightMachineHarness(t)
	m, e := h.m, h.m.endpoints["local"]
	cfg, err := config.Decode([]byte("onboarding=false\n[[keys.command]]\nkey='prefix+t'\ntype='popup'\ncommand='exec /bin/cat'\nwidth='80%'\nheight='80%'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.connection.enqueue(msg{Type: "view_config", Config: &cfg}); err != nil {
		t.Fatal(err)
	}
	h.until("configured scratch key", func() bool { return len(e.snapshot.SidebarConfig.Keys.Command) > 0 })
	for _, event := range []input.Event{{Kind: input.KindKey, Key: uv.Key{Code: 'b', Mod: uv.ModCtrl}, Raw: []byte{2}}, {Kind: input.KindKey, Key: uv.Key{Code: 't'}, Raw: []byte("t")}} {
		if err := m.input(event); err != nil {
			t.Fatal(err)
		}
	}
	h.until("scratch popup", func() bool { return e.snapshot.State.PopupVisible() })
	scratch := e.snapshot.State.Popup.PaneID
	h.api("local", api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "command_guard", Title: "guard approval", Origin: &api.DecisionOrigin{PaneID: "p1", Command: "true"}, Options: []api.DecisionOption{{ID: "approve", Label: "approve", Role: "approve"}}})
	h.until("indicator", func() bool { return len(e.guardApproval.pending) == 1 && m.regions.AnswerRects[1].W > 0 })
	r := m.regions.AnswerRects[1]
	if err := m.input(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: r.X, Y: r.Y, Button: uv.MouseLeft}}); err != nil {
		t.Fatal(err)
	}
	if e.guardApproval.open == "" || !e.snapshot.State.PopupVisible() || e.snapshot.State.Popup.PaneID != scratch {
		t.Fatal("approval replaced scratch popup")
	}
	if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEscape}}); err != nil {
		t.Fatal(err)
	}
	if e.guardApproval.open != "" || !e.snapshot.State.PopupVisible() || e.snapshot.State.Popup.PaneID != scratch {
		t.Fatal("escape closed scratch popup")
	}
}

func TestGuardApprovalTracksIncompletePaste(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var state machineInputState
	events, stop, err := readMachineInputWithState(reader, ctx, &state)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err := writer.WriteString("\x1b[200~hello"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !state.pending.Load() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if !state.pending.Load() {
		t.Fatal("partial paste was not marked as active input")
	}
	if _, err := writer.WriteString("\x1b[201~"); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Kind != input.KindPaste || event.Text != "hello" {
			t.Fatalf("paste = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("paste was not delivered")
	}
}

func TestGuardApprovalUpdatesBothDisplayClients(t *testing.T) {
	h := newRightMachineHarness(t)
	first := h.m
	stream, err := net.Dial("unix", api.ClientSocketPath(h.dirs["local"]))
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.CreateTemp(t.TempDir(), "second-display-")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	ctx, cancel := context.WithCancel(context.Background())
	second := &machineClient{ctx: ctx, session: first.session, active: "local", order: []string{"local"}, out: out, settings: first.settings, size: first.size, events: make(chan endpointEvent, 128), endpoints: map[string]*managedEndpoint{"local": {profile: machine.Profile{ID: "local", Enabled: true, Session: first.session}}}}
	second.start("local", stream)
	defer func() {
		cancel()
		for _, e := range second.endpoints {
			if e.cancel != nil {
				e.cancel()
			}
		}
		second.workers.Wait()
	}()
	h2 := &rightMachineHarness{m: second, t: t, dirs: h.dirs}
	h2.until("second display", func() bool { return second.endpoints["local"].snapshot != nil })
	h.api("local", api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "command_guard", Title: "two displays", Origin: &api.DecisionOrigin{PaneID: "p1", Command: "true"}, Options: []api.DecisionOption{{ID: "approve", Label: "approve", Role: "approve"}}})
	h.until("first pending", func() bool { return len(first.endpoints["local"].guardApproval.pending) == 1 })
	h2.until("second pending", func() bool { return len(second.endpoints["local"].guardApproval.pending) == 1 })
	if first.endpoints["local"].guardApproval.open == "" || second.endpoints["local"].guardApproval.open == "" {
		t.Fatal("both displays must show active guard")
	}
	if err := first.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: '1'}}); err != nil {
		t.Fatal(err)
	}
	if err := first.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEnter}}); err != nil {
		t.Fatal(err)
	}
	h.until("first resolved", func() bool { return len(first.endpoints["local"].guardApproval.pending) == 0 })
	h2.until("second resolved", func() bool { return len(second.endpoints["local"].guardApproval.pending) == 0 })
	if second.endpoints["local"].guardApproval.open != "" {
		t.Fatal("other display retained answered decision")
	}
}

func TestGuardApprovalFollowsRemoteMachineSelection(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	remote := m.endpoints["remote"]
	h.api("remote", api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "command_guard", Title: "remote guard", Origin: &api.DecisionOrigin{PaneID: "p1", Command: "true"}, Options: []api.DecisionOption{{ID: "approve", Label: "approve", Role: "approve"}}})
	h.until("remote pending", func() bool { return len(remote.guardApproval.pending) == 1 })
	if remote.guardApproval.open != "" || len(m.endpoints["local"].guardApproval.pending) != 0 {
		t.Fatal("remote request leaked into local display")
	}
	m.pending = &pendingMachine{id: "remote", workspace: remote.snapshot.State.ActiveWorkspace().ID, deadline: time.Now().Add(machineSwitchWait)}
	m.requestPending()
	h.until("remote selected", func() bool { return m.active == "remote" && m.pending == nil && remote.guardApproval.open != "" })
	if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: '1'}}); err != nil {
		t.Fatal(err)
	}
	if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEnter}}); err != nil {
		t.Fatal(err)
	}
	h.until("remote answered", func() bool { return len(remote.guardApproval.pending) == 0 })
	if len(m.endpoints["local"].guardApproval.pending) != 0 {
		t.Fatal("remote answer changed local decisions")
	}
}

func TestGuardApprovalRemappedKeyThroughRealInput(t *testing.T) {
	h := newRightMachineHarness(t)
	m, e := h.m, h.m.endpoints["local"]
	cfg, err := config.Decode([]byte("onboarding=false\n[keys]\nopen_guard_approval='cmd+y'\n"))
	if err != nil || len(cfg.Warnings) != 0 {
		t.Fatal(cfg.Warnings, err)
	}
	if err := e.connection.enqueue(msg{Type: "view_config", Config: &cfg}); err != nil {
		t.Fatal(err)
	}
	h.until("remapped config", func() bool {
		v := e.snapshot.SidebarConfig.ActionBindings("open_guard_approval")
		return len(v) == 1 && v[0] == "cmd+y"
	})
	h.api("local", api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "command_guard", Title: "guard", Origin: &api.DecisionOrigin{PaneID: "p1", Command: "true"}, Options: []api.DecisionOption{{ID: "approve", Label: "approve", Role: "approve"}}})
	h.until("guard open", func() bool { return e.guardApproval.open != "" })
	if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEscape}}); err != nil {
		t.Fatal(err)
	}
	framer := &input.Framer{}
	feed := func(bytes string) {
		t.Helper()
		for _, event := range framer.Push([]byte(bytes)) {
			if err := m.input(event); err != nil {
				t.Fatal(err)
			}
		}
	}
	feed("\x02a")
	if e.guardApproval.open != "" {
		t.Fatal("old binding reopened approval")
	}
	feed("\x1b[121;9u")
	if e.guardApproval.open == "" {
		t.Fatal("remapped key did not open approval")
	}
}
