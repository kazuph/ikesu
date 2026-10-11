package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecisionAskInsidePaneOrigin(t *testing.T) {
	s := start(t)
	s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.send(fmt.Sprintf("%q decision ask --title InsidePane --option yes:Yes:approve\r", binPath))
	var list api.DecisionListResult
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := s.cli("decision", "list", "--status", "pending")
		if err != nil {
			t.Fatal(raw, err)
		}
		var response api.Response
		if err = json.Unmarshal([]byte(raw), &response); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(response.Result, &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Decisions) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pending missing")
		}
		s.pump(time.Now().Add(10 * time.Millisecond))
	}
	d := list.Decisions[0]
	if d.Origin == nil || d.Origin.PaneID != "p1" {
		t.Fatal(d)
	}
	if raw, err := s.cli("decision", "answer", d.DecisionID, "yes"); err != nil {
		t.Fatal(raw, err)
	}
	s.waitFor("answered", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "answered") })
}
func TestDecisionDialogActualFlow(t *testing.T) {
	s := start(t)
	s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	conn, err := net.Dial("unix", filepath.Join(s.stateDir, s.name, "gtx.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	request := api.Request{ID: "ui", Method: api.MethodDecisionCreate, Params: api.MarshalParams(api.DecisionCreateParams{Kind: "ask", Title: "DecisionUI", Body: ptrDecisionText("Detailed body"), AllowText: true, Options: []api.DecisionOption{{ID: "yes", Label: "ApproveUI", Role: "approve"}, {ID: "no", Label: "RejectUI", Role: "reject"}}})}
	if err := api.WriteRequest(conn, request); err != nil {
		t.Fatal(err)
	}
	response, err := api.ReadResponse(bufio.NewReader(conn))
	if err != nil || response.Err != nil {
		t.Fatal(response, err)
	}
	var result api.DecisionResult
	json.Unmarshal(response.Result, &result)
	id := result.Decision.DecisionID
	s.waitFor("dialog", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "DecisionUI") && strings.Contains(tx, "ApproveUI") && strings.Contains(tx, "Local · 1/1")
	})
	s.send("printf 'INPUT_%s\\n' KEPT\r")
	s.waitFor("pane input while dialog open", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "INPUT_KEPT") })
	raw, err := s.cli("decision", "get", id)
	if err != nil || !strings.Contains(raw, "pending") {
		t.Fatal("pane enter answered", raw, err)
	}
	s.send("\x1b")
	s.waitFor("closed indicator", 5*time.Second, func(tx string) bool { return !strings.Contains(tx, "ApproveUI") && strings.Contains(tx, "! 1") })
	x, y := s.find("! 1")
	s.click(x, y)
	s.waitFor("reopened", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "ApproveUI") })
	x, y = s.find("Detailed body")
	s.click(x, y)
	s.send("client typed\x1b[B\r")
	s.waitFor("resolved dialog removed", 5*time.Second, func(tx string) bool { return !strings.Contains(tx, "ApproveUI") && !strings.Contains(tx, "! 1") })
	raw, err = s.cli("decision", "get", id)
	if err != nil {
		t.Fatal(raw, err)
	}
	json.Unmarshal([]byte(raw), &response)
	json.Unmarshal(response.Result, &result)
	if result.Decision.Answer == nil || result.Decision.Answer.Responder != "client" || *result.Decision.Answer.OptionID != "no" || result.Decision.Answer.Text == nil || *result.Decision.Answer.Text != "client typed" {
		t.Fatal(result)
	}
	request.Params = api.MarshalParams(api.DecisionCreateParams{Kind: "ask", Title: "ExternalUI", Options: []api.DecisionOption{{ID: "yes", Label: "ExternalOption", Role: "approve"}}})
	api.WriteRequest(conn, request)
	response, err = api.ReadResponse(bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(response.Result, &result)
	s.waitFor("external pending", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "ExternalOption") })
	if raw, err := s.cli("decision", "cancel", result.Decision.DecisionID); err != nil {
		t.Fatal(raw, err)
	}
	s.waitFor("external resolved removed", 5*time.Second, func(tx string) bool { return !strings.Contains(tx, "ExternalOption") && !strings.Contains(tx, "! 1") })
}
func ptrDecisionText(s string) *string { return &s }
