package client

import (
	"testing"

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
