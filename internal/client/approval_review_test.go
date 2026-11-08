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
)

func reviewApproval() api.Decision {
	return api.Decision{DecisionID: "d1", Kind: "command_guard", Status: "pending", AllowText: true, Origin: &api.DecisionOrigin{PaneID: "p1"}, Options: []api.DecisionOption{{ID: "approve", Label: "approve", Role: "approve"}, {ID: "day_allow", Label: "day", Role: "approve"}, {ID: "reject", Label: "reject", Role: "reject"}}}
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

func TestLateAnswerInputSelectionAndText(t *testing.T) {
	h := newRightMachineHarness(t)
	m, e := h.m, h.m.endpoints["local"]
	d := api.Decision{DecisionID: "late", Kind: "agent_prompt", Status: "pending", LateAnswer: true, AllowText: true, MultiSelect: true, Origin: &api.DecisionOrigin{PaneID: "p1"}, Options: []api.DecisionOption{{ID: "1", Label: "Red"}, {ID: "2", Label: "Blue"}}}
	e.guardApproval.update(&endpoint.DecisionProjection{BootID: "late", Revision: 1, Pending: []api.Decision{d}}, "p1", time.Now(), time.Time{})
	e.guardApproval.openPane("p1")
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	key := func(r rune) {
		t.Helper()
		if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: r, Text: string(r)}, Raw: []byte(string(r))}); err != nil {
			t.Fatal(err)
		}
	}
	key('1')
	key(' ')
	key('2')
	key(' ')
	key('t')
	key('1')
	key('2')
	s := &e.guardApproval
	if !s.checked["1"] || !s.checked["2"] || s.selected != "2" || s.reason != "12" {
		t.Fatalf("selection=%q checked=%v text=%q", s.selected, s.checked, s.reason)
	}
	if s.waiting {
		t.Fatal("selection sent before Enter")
	}
	if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEscape}, Raw: []byte("\x1b")}); err != nil {
		t.Fatal(err)
	}
	if s.open != "" || len(s.pending) != 1 {
		t.Fatal("escape removed pending question")
	}
}
