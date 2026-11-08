package client

import (
	"context"

	"os"
	"runtime"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/input"
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
