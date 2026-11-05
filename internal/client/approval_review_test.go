package client

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func reviewApproval() api.Decision {
	return api.Decision{DecisionID: "d1", Kind: "command_guard", Status: "pending", AllowText: true, Origin: &api.DecisionOrigin{PaneID: "p1"}, Options: []api.DecisionOption{{ID: "approve", Label: "approve", Role: "approve"}, {ID: "day_allow", Label: "day", Role: "approve"}, {ID: "reject", Label: "reject", Role: "reject"}}}
}

func TestReviewClippedApprovalDoesNotCaptureKeys(t *testing.T) {
	h := newRightMachineHarness(t)
	m, e := h.m, h.m.endpoints["local"]
	d := reviewApproval()
	h.api("local", api.MethodDecisionCreate, api.DecisionCreateParams{Kind: d.Kind, Title: "clipped", Origin: d.Origin, Options: d.Options})
	h.until("approval drawn", func() bool { return e.guardApproval.painted != "" })
	m.size = tui.Winsize{Cols: 1, Rows: 1}
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	if e.guardApproval.painted != "" {
		t.Fatal("off-screen approval marked painted")
	}
}

func TestReviewRejectReasonNumbers(t *testing.T) {
	h := newRightMachineHarness(t)
	m, e := h.m, h.m.endpoints["local"]
	d := reviewApproval()
	h.api("local", api.MethodDecisionCreate, api.DecisionCreateParams{Kind: d.Kind, Title: "review", Origin: d.Origin, Options: d.Options, AllowText: true})
	h.until("approval drawn", func() bool { return e.guardApproval.current() != nil && m.regions.ApprovalRect.W > 0 })
	for _, r := range "312" {
		if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: r, Text: string(r)}, Raw: []byte(string(r))}); err != nil {
			t.Fatal(err)
		}
	}
	if e.guardApproval.selected != "reject" || e.guardApproval.reason != "12" {
		t.Fatalf("selected=%q reason=%q", e.guardApproval.selected, e.guardApproval.reason)
	}
	id := e.guardApproval.open
	if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEnter}, Raw: []byte("\r")}); err != nil {
		t.Fatal(err)
	}
	h.until("rejection propagated", func() bool { return len(e.guardApproval.pending) == 0 })
	conn, err := net.DialTimeout("unix", api.APISocketPath(h.dirs["local"]), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := api.WriteRequest(conn, api.Request{ID: "review", Method: api.MethodDecisionGet, Params: api.MarshalParams(api.DecisionTargetParams{DecisionID: id})}); err != nil {
		t.Fatal(err)
	}
	response, err := api.ReadResponse(bufio.NewReader(conn))
	if err != nil || response.Err != nil {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	var result api.DecisionResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	answer := result.Decision.Answer
	if answer == nil || answer.OptionID == nil || *answer.OptionID != "reject" || answer.Text == nil || *answer.Text != "12" {
		t.Fatalf("answer=%+v", answer)
	}
}

func TestReviewUnpaintedApprovalDoesNotCaptureKeys(t *testing.T) {
	s := model.New(t.TempDir(), 120, 40)
	e := &managedEndpoint{snapshot: &render.Input{State: s}}
	e.guardApproval.update(&endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{reviewApproval()}}, "p1", time.Now(), time.Time{})
	m := &machineClient{active: "local", endpoints: map[string]*managedEndpoint{"local": e}}
	m.outputFence.Written([]byte("\x1b_Ga=t,"), time.Now())
	handled, err := m.approvalInputEvent(input.Event{Kind: input.KindKey, Key: uv.Key{Code: '1'}, Raw: []byte("1")})
	if err != nil || handled || e.guardApproval.selected != "" || e.guardApproval.open != "" {
		t.Fatalf("handled=%v err=%v selected=%q open=%q", handled, err, e.guardApproval.selected, e.guardApproval.open)
	}
}

func TestReviewOriginMachineFilter(t *testing.T) {
	local, foreign := reviewApproval(), reviewApproval()
	foreign.DecisionID = "other"
	foreign.Origin.Machine = "mini"
	var s approvalState
	s.update(&endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{foreign, local}}, "p1", time.Now(), time.Time{})
	if len(s.pending) != 1 || s.pending[0].DecisionID != "d1" {
		t.Fatalf("pending=%+v", s.pending)
	}
}

func TestReviewForeignMachineProjectionThroughServer(t *testing.T) {
	h := newRightMachineHarness(t)
	m, e := h.m, h.m.endpoints["local"]
	d := reviewApproval()
	foreign := *d.Origin
	foreign.Machine = "mini"
	h.api("local", api.MethodDecisionCreate, api.DecisionCreateParams{Kind: d.Kind, Title: "foreign", Origin: &foreign, Options: d.Options})
	h.api("local", api.MethodDecisionCreate, api.DecisionCreateParams{Kind: d.Kind, Title: "local", Origin: d.Origin, Options: d.Options})
	h.until("local decision", func() bool { return len(e.guardApproval.pending) > 0 })
	if len(e.guardApproval.pending) != 1 || e.guardApproval.pending[0].Title != "local" || e.guardApproval.open != e.guardApproval.pending[0].DecisionID {
		t.Fatalf("pending=%+v open=%q", e.guardApproval.pending, e.guardApproval.open)
	}
	if m.regions.AnswerRects[1].W == 0 {
		t.Fatal("local indicator missing")
	}
}

func TestReviewFenceBeforeApprovalPaintThroughServer(t *testing.T) {
	h := newRightMachineHarness(t)
	m, e := h.m, h.m.endpoints["local"]
	d := reviewApproval()
	if !m.outputFence.Written([]byte("\x1b_Ga=t,"), time.Now()) {
		t.Fatal("fence not started")
	}
	h.api("local", api.MethodDecisionCreate, api.DecisionCreateParams{Kind: d.Kind, Title: "fence", Origin: d.Origin, Options: d.Options})
	timer := time.NewTimer(800 * time.Millisecond)
	defer timer.Stop()
	for e.guardApproval.current() == nil {
		select {
		case ev := <-m.events:
			if ev.err != nil {
				t.Fatal(ev.err)
			}
			if _, done := m.event(ev); done {
				t.Fatal("endpoint ended")
			}
		case <-timer.C:
			t.Fatal("decision did not arrive before fence expired")
		}
	}
	if e.guardApproval.painted != "" {
		t.Fatal("approval painted while fence was pending")
	}
	for _, ev := range []input.Event{{Kind: input.KindKey, Key: uv.Key{Code: '1', Text: "1"}, Raw: []byte("1")}, {Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEnter}, Raw: []byte("\r")}} {
		if err := m.input(ev); err != nil {
			t.Fatal(err)
		}
	}
	if e.guardApproval.open != "" || len(e.guardApproval.pending) != 1 {
		t.Fatal("invisible approval accepted input")
	}
	if err := m.input(input.Event{Kind: input.KindUnknown, Raw: []byte("\x1b[0n")}); err != nil {
		t.Fatal(err)
	}
	e.guardApproval.openPane("p1")
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	if e.guardApproval.painted != e.guardApproval.open {
		t.Fatal("approval not painted")
	}
	for _, ev := range []input.Event{{Kind: input.KindKey, Key: uv.Key{Code: '1', Text: "1"}, Raw: []byte("1")}, {Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEnter}, Raw: []byte("\r")}} {
		if err := m.input(ev); err != nil {
			t.Fatal(err)
		}
	}
	h.until("answer after paint", func() bool { return len(e.guardApproval.pending) == 0 })
}
