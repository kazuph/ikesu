package client

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/machine"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"testing"
)

func decisionConnection() *endpointConnection {
	p := endpoint.Current()
	return &endpointConnection{writer: &writer{available: p.Generation, methods: p.Methods}, wake: make(chan struct{}, 1)}
}
func decisionFixture() *machineClient {
	return &machineClient{active: "local", endpoints: map[string]*managedEndpoint{
		"local":  {profile: machine.Profile{ID: "local", Label: "Local", Enabled: true}, connection: decisionConnection()},
		"remote": {profile: machine.Profile{ID: "remote", Label: "Mini", Enabled: true}, connection: decisionConnection()},
	}}
}
func pendingDecision(id string, created int64) api.Decision {
	return api.Decision{DecisionID: id, Title: id, Kind: "ask", Status: "pending", CreatedUnixMS: created, Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}, {ID: "no", Label: "No", Role: "reject"}}}
}
func TestDecisionQueueProjectionOrderCloseResolveDisconnect(t *testing.T) {
	m := decisionFixture()
	a := pendingDecision("dec-a", 2)
	b := pendingDecision("dec-b", 1)
	m.acceptDecisions("local", &endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{a}})
	m.acceptDecisions("remote", &endpoint.DecisionProjection{BootID: "b", Revision: 1, Pending: []api.Decision{b}})
	if len(m.decisions.queue) != 2 || m.currentDecision().record.DecisionID != "dec-b" {
		t.Fatal(m.decisions)
	}
	handled, err := m.decisionInput(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEscape}})
	if !handled || err != nil || !m.decisions.closed {
		t.Fatal(m.decisions)
	}
	out := render.Compose(render.Input{State: model.New("/tmp", 80, 24)})
	m.paintDecisions(out)
	marker := m.decisions.regions.Marker
	handled, _ = m.decisionInput(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{Button: uv.MouseLeft, X: marker.X, Y: marker.Y}})
	if !handled || m.decisions.closed || m.decisions.focused {
		t.Fatal(m.decisions)
	}
	m.acceptDecisions("remote", &endpoint.DecisionProjection{BootID: "b", Revision: 2, Pending: []api.Decision{}})
	if len(m.decisions.queue) != 1 || m.currentDecision().record.DecisionID != "dec-a" {
		t.Fatal(m.decisions)
	}
	m.acceptDecisions("remote", &endpoint.DecisionProjection{BootID: "b", Revision: 1, Pending: []api.Decision{b}})
	if len(m.decisions.queue) != 1 {
		t.Fatal("stale projection restored decision")
	}
	m.endpoints["local"].connection = nil
	m.updateDecisionQueue()
	if len(m.decisions.queue) != 0 {
		t.Fatal("disconnected decision retained")
	}
	m.endpoints["local"].connection = decisionConnection()
	m.acceptDecisions("local", &endpoint.DecisionProjection{BootID: "new", Revision: 1, Pending: []api.Decision{a}})
	if len(m.decisions.queue) != 1 {
		t.Fatal("restart projection missing")
	}
}
func TestDecisionKeyboardFocusAndOwnerAnswer(t *testing.T) {
	m := decisionFixture()
	d := pendingDecision("dec-remote", 1)
	d.AllowText = true
	m.acceptDecisions("remote", &endpoint.DecisionProjection{BootID: "b", Revision: 1, Pending: []api.Decision{d}})
	m.decisions.regions.Outer = layout.Rect{X: 10, Y: 5, W: 30, H: 10}
	enter := input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEnter}, Raw: []byte("\r")}
	if handled, _ := m.decisionInput(enter); handled {
		t.Fatal("enter stolen before click")
	}
	if handled, _ := m.decisionInput(input.Event{Kind: input.KindKey, Key: uv.Key{Code: 'x', Text: "x"}}); handled {
		t.Fatal("typing stolen before click")
	}
	if handled, _ := m.decisionInput(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{Button: uv.MouseLeft, X: 11, Y: 6}}); !handled {
		t.Fatal("focus click missing")
	}
	m.decisionInput(input.Event{Kind: input.KindKey, Key: uv.Key{Code: 'x', Text: "answer"}})
	m.decisionInput(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyDown}})
	if handled, err := m.decisionInput(enter); !handled || err != nil {
		t.Fatal(handled, err)
	}
	if len(m.endpoints["local"].connection.queue) != 0 {
		t.Fatal("answer sent to presenting endpoint")
	}
	messages := m.endpoints["remote"].connection.queue
	if len(messages) != 1 || messages[0].Type != "decision_answer" {
		t.Fatal(messages)
	}
	p := messages[0].DecisionAnswer
	if p.Responder != "client" || *p.OptionID != "no" || p.Text == nil || *p.Text != "answer" {
		t.Fatal(p)
	}
	m.decisionInput(enter)
	if len(m.endpoints["remote"].connection.queue) != 1 {
		t.Fatal("double submission")
	}
	if endpoint.RequiredGeneration("decision_answer") != 3 || endpoint.MinGeneration != 1 {
		t.Fatal("generation contract")
	}
	old := decisionConnection()
	old.writer.available = 2
	if err := old.enqueue(messages[0]); err == nil {
		t.Fatal("old endpoint silently accepted decision answer")
	}
}
