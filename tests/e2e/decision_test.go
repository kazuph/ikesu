package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
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
func TestDecisionPendingInvisibleAndCLIAnswer(t *testing.T) {
	s := start(t)
	s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	conn, err := net.Dial("unix", filepath.Join(s.stateDir, s.name, "gtx.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	create := func(title string) string {
		t.Helper()
		request := api.Request{ID: "hidden", Method: api.MethodDecisionCreate, Params: api.MarshalParams(api.DecisionCreateParams{Kind: "ask", Title: title, Body: ptrDecisionText("HiddenDecisionBody"), AllowText: true, Options: []api.DecisionOption{{ID: "yes", Label: "HiddenApprove", Role: "approve"}, {ID: "no", Label: "HiddenReject", Role: "reject"}}})}
		if err := api.WriteRequest(conn, request); err != nil {
			t.Fatal(err)
		}
		response, err := api.ReadResponse(reader)
		if err != nil || response.Err != nil {
			t.Fatal(response, err)
		}
		var result api.DecisionResult
		if err := json.Unmarshal(response.Result, &result); err != nil {
			t.Fatal(err)
		}
		return result.Decision.DecisionID
	}
	assertInvisible := func() {
		t.Helper()
		for _, forbidden := range []string{"HiddenDecision", "HiddenApprove", "HiddenReject", "! 1", "! 2", "esc close"} {
			if strings.Contains(s.text(), forbidden) {
				t.Fatalf("pending UI leaked %q:\n%s", forbidden, s.text())
			}
		}
	}
	id := create("HiddenDecisionTitle")
	second := create("HiddenDecisionSecond")
	s.send("printf 'INPUT_%s\\n' KEPT\r")
	s.waitFor("pane input while pending", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "INPUT_KEPT") })
	assertInvisible()
	for _, pending := range []string{id, second} {
		if raw, err := s.cli("decision", "get", pending); err != nil || !strings.Contains(raw, `"pending"`) {
			t.Fatal("pane input resolved decision", raw, err)
		}
	}
	s.send("\x1b")
	s.pump(time.Now().Add(100 * time.Millisecond))
	assertInvisible()
	s.send("\x03")
	s.pump(time.Now().Add(100 * time.Millisecond))
	if raw, err := s.cli("decision", "answer", id, "no", "--text", "CLI rejection"); err != nil {
		t.Fatal(raw, err)
	}
	raw, err := s.cli("decision", "get", id)
	if err != nil {
		t.Fatal(raw, err)
	}
	var response api.Response
	var result api.DecisionResult
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision.Answer == nil || result.Decision.Answer.OptionID == nil || *result.Decision.Answer.OptionID != "no" || result.Decision.Answer.Text == nil || *result.Decision.Answer.Text != "CLI rejection" {
		t.Fatal(result)
	}
	if raw, err := s.cli("decision", "cancel", second); err != nil {
		t.Fatal(raw, err)
	}
	s.send("printf 'AFTER_%s\\n' RESOLVED\r")
	s.waitFor("pane input after external resolution", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "AFTER_RESOLVED") })
	assertInvisible()
}
func ptrDecisionText(s string) *string { return &s }
