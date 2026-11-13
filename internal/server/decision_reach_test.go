package server

import (
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/cli"
	"github.com/kazuph/ikesu/internal/teststate"
)

func reach(t *testing.T, dir, id, channel, state string) api.Decision {
	t.Helper()
	var result api.DecisionResult
	apiOK(t, dir, api.MethodDecisionReach, api.DecisionReachParams{DecisionID: id, Channel: channel, State: state}, &result)
	return result.Decision
}

func TestDecisionReachRecordsNotifiesAndIsIdempotent(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	d := createDecision(t, dir, nil)
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer write.Close()
	read.SetReadDeadline(time.Now().Add(10 * time.Second))
	go func() {
		cli.Run([]string{"decision", "watch"}, cli.Options{Session: "test", Explicit: true, Stdout: write, Stderr: io.Discard})
	}()
	dec := json.NewDecoder(read)
	var row struct {
		Event    string       `json:"event"`
		Decision api.Decision `json:"decision"`
	}
	for _, event := range []string{"machine_up", "created"} {
		if err := dec.Decode(&row); err != nil || row.Event != event {
			t.Fatalf("initial %s: %+v %v", event, row, err)
		}
	}
	first := reach(t, dir, d.DecisionID, "android", api.ReachNoClient)
	if first.Reach["android"].State != api.ReachNoClient || first.Reach["android"].AtUnixMS == 0 {
		t.Fatalf("no_client: %+v", first.Reach)
	}
	// プラグイン（watch）の onUpdated に届く。
	if err := dec.Decode(&row); err != nil || row.Event != "updated" || row.Decision.Reach["android"].State != api.ReachNoClient {
		t.Fatalf("updated: %+v %v", row, err)
	}
	at := first.Reach["android"].AtUnixMS
	time.Sleep(5 * time.Millisecond)
	shown := reach(t, dir, d.DecisionID, "android", api.ReachShown)
	if shown.Reach["android"].State != api.ReachShown || shown.Reach["android"].AtUnixMS != at {
		t.Fatalf("shown keeps first at: %+v (at=%d)", shown.Reach, at)
	}
	// shown のあとに no_client が来ても shown のまま。同じ呼び出しを繰り返してもよい。
	for _, state := range []string{api.ReachNoClient, api.ReachShown} {
		again := reach(t, dir, d.DecisionID, "android", state)
		if again.Reach["android"].State != api.ReachShown || again.Reach["android"].AtUnixMS != at {
			t.Fatalf("%s: %+v", state, again.Reach)
		}
	}
	var got api.DecisionResult
	apiOK(t, dir, api.MethodDecisionGet, api.DecisionTargetParams{DecisionID: d.DecisionID}, &got)
	if got.Decision.Reach["android"].State != api.ReachShown {
		t.Fatalf("get: %+v", got.Decision.Reach)
	}
	// 検証
	for _, p := range []api.DecisionReachParams{{DecisionID: d.DecisionID, Channel: "android", State: "bogus"}, {DecisionID: d.DecisionID, Channel: "", State: api.ReachShown}} {
		if resp := apiReq(t, dir, api.MethodDecisionReach, p); resp.Err == nil {
			t.Fatalf("invalid %+v accepted", p)
		}
	}
	if resp := apiReq(t, dir, api.MethodDecisionReach, api.DecisionReachParams{DecisionID: "dec-none", Channel: "android", State: api.ReachShown}); resp.Err == nil || resp.Err.Code != api.ErrDecisionNotFound {
		t.Fatalf("unknown id: %+v", resp.Err)
	}
}

func TestDecisionReachOnResolvedDecisionSucceedsWithoutRecording(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	d := createDecision(t, dir, nil)
	apiOK(t, dir, api.MethodDecisionCancel, api.DecisionTargetParams{DecisionID: d.DecisionID}, nil)
	after := reach(t, dir, d.DecisionID, "android", api.ReachShown)
	if len(after.Reach) != 0 || after.Status != "cancelled" {
		t.Fatalf("recorded on resolved decision: %+v", after)
	}
}
