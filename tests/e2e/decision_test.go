package e2e

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
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
