package cli

import (
	"context"
	"encoding/json"
	"github.com/kazuph/goterrace/internal/api"
	"os"
	"testing"
	"time"
)

func TestDecisionWatchLineContractAndRetry(t *testing.T) {
	record := api.Decision{DecisionID: "dec-test", Kind: "ask", Title: "Whole", Status: "pending", Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}}
	owner := decisionMachine{"saved", "Mini"}
	for _, event := range []string{"created", "resolved", "machine_up", "machine_down"} {
		row := decisionWatchLine{Event: event, Machine: owner}
		if event == "created" || event == "resolved" {
			row.Decision = &record
		}
		if event == "machine_down" {
			row.Reason = "disconnected"
		}
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			t.Fatal(string(raw))
		}
		if len(obj) != map[string]int{"created": 3, "resolved": 3, "machine_up": 2, "machine_down": 3}[event] {
			t.Fatal(string(raw))
		}
		var got decisionWatchLine
		if json.Unmarshal(raw, &got) != nil || got.Machine != owner {
			t.Fatal(string(raw))
		}
		if row.Decision != nil && (got.Decision.Title != record.Title || len(got.Decision.Options) != 1) {
			t.Fatal(string(raw))
		}
	}
	if watchRetry(0, false) != decisionRetryInitial || watchRetry(decisionRetryInitial, false) != 2*decisionRetryInitial || watchRetry(decisionRetryLimit, false) != decisionRetryLimit || watchRetry(time.Second, true) != 10*time.Minute {
		t.Fatal("retry contract")
	}
	reason, auth := decisionAuthReason("To authenticate, visit: https://login.example/test\n")
	if !auth || reason != "SSH authentication required: https://login.example/test" {
		t.Fatal(reason, auth)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitDecisionRetry(ctx, 10*time.Minute) {
		t.Fatal("retry ignored cancellation")
	}
}
func TestDecisionWatchIdlePipeClose(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer write.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := watchOutputClosed(write, cancel)
	defer stop()
	read.Close()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("idle pipe close did not cancel")
	}
}
