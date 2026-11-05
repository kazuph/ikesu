package client

import (
	"encoding/json"
	"testing"
)

// The display must retain the server's pending guard request so it can render
// the same decision and answer it through the existing protocol.
func TestGuardDecisionProjectionSurvivesClientDecode(t *testing.T) {
	const wire = `{"type":"decisions","decisions":{"boot_id":"boot","revision":1,"pending":[{"decision_id":"d1","kind":"command_guard","status":"pending","origin":{"pane_id":"p1","command":"true"}}]}}`
	var message msg
	if err := json.Unmarshal([]byte(wire), &message); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Decisions struct {
			Pending []struct {
				ID string `json:"decision_id"`
			} `json:"pending"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Decisions.Pending) != 1 || decoded.Decisions.Pending[0].ID != "d1" {
		t.Fatalf("pending guard decision lost during client decode: %s", encoded)
	}
}
