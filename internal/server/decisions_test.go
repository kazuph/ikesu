package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/cli"
	"github.com/kazuph/ikesu/internal/plugin"
	"github.com/kazuph/ikesu/internal/teststate"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func createDecision(t *testing.T, dir string, timeout *uint64) api.Decision {
	t.Helper()
	var result api.DecisionResult
	apiOK(t, dir, api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "ask", Title: "Proceed?", Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}, TimeoutMS: timeout}, &result)
	return result.Decision
}
func decisionStream(t *testing.T, dir, method string, p any) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("unix", api.APISocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := api.WriteRequest(conn, api.Request{ID: "test", Method: method, Params: api.MarshalParams(p)}); err != nil {
		t.Fatal(err)
	}
	return conn, bufio.NewReader(conn)
}
func TestDecisionHubAPIWaitAndEvents(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	ready := dialClient(t, dir)
	ready.conn.Close()
	defer stopServer(t, dir, done)
	_, events := decisionStream(t, dir, api.MethodEventsSubscribe, api.EventsSubscribeParams{Subscriptions: []api.Subscription{{Type: "decision.created"}, {Type: "decision.resolved"}}})
	if _, err := events.ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	d := createDecision(t, dir, nil)
	raw, err := events.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var env api.EventEnvelope
	json.Unmarshal(raw, &env)
	var eventDecision api.Decision
	json.Unmarshal(env.Data, &eventDecision)
	if env.Event != "decision_created" || eventDecision.DecisionID != d.DecisionID || eventDecision.Title != d.Title {
		t.Fatalf("created %s", raw)
	}
	zero := uint64(0)
	var result api.DecisionResult
	apiOK(t, dir, api.MethodDecisionWait, api.DecisionWaitParams{DecisionID: d.DecisionID, TimeoutMS: &zero}, &result)
	if result.Decision.Status != "pending" {
		t.Fatal(result)
	}
	_, wait := decisionStream(t, dir, api.MethodDecisionWait, api.DecisionWaitParams{DecisionID: d.DecisionID})
	yes := "yes"
	apiOK(t, dir, api.MethodDecisionAnswer, api.DecisionAnswerParams{DecisionID: d.DecisionID, OptionID: &yes, Responder: "test"}, &result)
	line, err := wait.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response api.Response
	json.Unmarshal(line, &response)
	json.Unmarshal(response.Result, &result)
	if result.Decision.Status != "answered" {
		t.Fatal(string(line))
	}
	raw, err = events.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(raw, &env)
	json.Unmarshal(env.Data, &eventDecision)
	if env.Event != "decision_resolved" || eventDecision.Answer == nil {
		t.Fatalf("resolved %s", raw)
	}
	second := apiReq(t, dir, api.MethodDecisionAnswer, api.DecisionAnswerParams{DecisionID: d.DecisionID, OptionID: &yes})
	if second.Err == nil || second.Err.Code != api.ErrDecisionResolved {
		t.Fatal(second)
	}
	d = createDecision(t, dir, nil)
	_, wait = decisionStream(t, dir, api.MethodDecisionWait, api.DecisionWaitParams{DecisionID: d.DecisionID})
	apiOK(t, dir, api.MethodDecisionCancel, api.DecisionTargetParams{DecisionID: d.DecisionID}, nil)
	line, err = wait.ReadBytes('\n')
	if err != nil || !bytes.Contains(line, []byte("cancelled")) {
		t.Fatalf("cancel wait %s %v", line, err)
	}
	ms := uint64(30)
	d = createDecision(t, dir, &ms)
	apiOK(t, dir, api.MethodDecisionWait, api.DecisionWaitParams{DecisionID: d.DecisionID}, &result)
	if result.Decision.Status != "expired" {
		t.Fatal(result)
	}
	d = createDecision(t, dir, nil)
	conn, _ := decisionStream(t, dir, api.MethodDecisionWait, api.DecisionWaitParams{DecisionID: d.DecisionID})
	conn.Close()
	apiOK(t, dir, api.MethodDecisionCancel, api.DecisionTargetParams{DecisionID: d.DecisionID}, nil)
}
func TestDecisionCLIRealServer(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	ready := dialClient(t, dir)
	ready.conn.Close()
	defer stopServer(t, dir, done)
	for _, status := range []string{"answered", "cancelled", "expired"} {
		var out, errs bytes.Buffer
		exit := make(chan int, 1)
		args := []string{"decision", "ask", "--title", status, "--option", "yes:Yes:approve"}
		if status == "expired" {
			args = append(args, "--timeout", "100")
		}
		go func() {
			exit <- cli.Run(args, cli.Options{Session: "test", Explicit: true, Stdout: &out, Stderr: &errs, Getenv: func(key string) string {
				if key == "GTX_PANE_ID" {
					return "p1"
				}
				return os.Getenv(key)
			}})
		}()
		var pending api.DecisionListResult
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			apiOK(t, dir, api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
			if len(pending.Decisions) > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if len(pending.Decisions) != 1 {
			t.Fatal(pending)
		}
		d := pending.Decisions[0]
		if d.Origin != nil {
			t.Fatal(d)
		}
		o := cli.Options{Session: "test", Explicit: true, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
		for _, command := range [][]string{{"decision", "get", d.DecisionID}, {"decision", "list", "--status", "pending"}} {
			if code := cli.Run(command, o); code != 0 {
				t.Fatal(code)
			}
		}
		switch status {
		case "answered":
			if code := cli.Run([]string{"decision", "answer", d.DecisionID, "yes"}, o); code != 0 {
				t.Fatal(code)
			}
		case "cancelled":
			if code := cli.Run([]string{"decision", "cancel", d.DecisionID}, o); code != 0 {
				t.Fatal(code)
			}
		}
		want := map[string]int{"answered": 0, "expired": 3, "cancelled": 4}[status]
		select {
		case got := <-exit:
			if got != want || !bytes.Contains(out.Bytes(), []byte(status)) {
				t.Fatalf("%s code %d out %s err %s", status, got, &out, &errs)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("ask did not finish")
		}
	}
}
func TestDecisionRestartPendingExpiry(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	ready := dialClient(t, dir)
	ready.conn.Close()
	ms := uint64(200)
	d := createDecision(t, dir, &ms)
	persistent := createDecision(t, dir, nil)
	stopServer(t, dir, done)
	done = make(chan int, 1)
	go func() {
		code, err := Run("test", "test")
		if err != nil {
			t.Error(err)
		}
		done <- code
	}()
	ready = dialClient(t, dir)
	ready.conn.Close()
	defer stopServer(t, dir, done)
	var result api.DecisionResult
	apiOK(t, dir, api.MethodDecisionGet, api.DecisionTargetParams{DecisionID: persistent.DecisionID}, &result)
	if result.Decision.Status != "pending" {
		t.Fatal(result)
	}
	apiOK(t, dir, api.MethodDecisionWait, api.DecisionWaitParams{DecisionID: d.DecisionID}, &result)
	if result.Decision.Status != "expired" {
		t.Fatal(result)
	}
}
func TestDecisionPluginFullRecord(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	defer stopServer(t, dir, done)
	root := t.TempDir()
	manifest := `id="decision.test"
name="Decision Test"
version="0.1.0"
min_gtx_version="0.1.0"
[[events]]
on="decision.created"
command=["/bin/sh","-c","printf '%s' \"$GTX_PLUGIN_EVENT_JSON\""]
[[events]]
on="decision.resolved"
command=["/bin/sh","-c","printf '%s' \"$GTX_PLUGIN_EVENT_JSON\""]
`
	if err := os.WriteFile(filepath.Join(root, "gtx-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	apiOK(t, dir, "plugin.link", map[string]any{"path": root}, nil)
	d := createDecision(t, dir, nil)
	apiOK(t, dir, api.MethodDecisionCancel, api.DecisionTargetParams{DecisionID: d.DecisionID}, nil)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var logs struct {
			Logs []plugin.CommandLog `json:"logs"`
		}
		apiOK(t, dir, "plugin.log.list", map[string]any{"plugin_id": "decision.test"}, &logs)
		count := 0
		for _, l := range logs.Logs {
			if l.Status != "succeeded" {
				continue
			}
			var record api.Decision
			if l.Stdout == nil || json.Unmarshal([]byte(*l.Stdout), &record) != nil || record.DecisionID != d.DecisionID || len(record.Options) != 1 {
				t.Fatalf("hook record %v", l)
			}
			if l.Event != "decision.created" && l.Event != "decision.resolved" {
				t.Fatal(l.Event)
			}
			count++
		}
		if count == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(logs)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestDecisionAskReconnectKeepsID(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	ready := dialClient(t, dir)
	ready.conn.Close()
	var out, errs bytes.Buffer
	exit := make(chan int, 1)
	go func() {
		exit <- cli.Run([]string{"decision", "ask", "--title", "Reconnect", "--option", "yes:Yes:approve"}, cli.Options{Session: "test", Explicit: true, Stdout: &out, Stderr: &errs})
	}()
	var list api.DecisionListResult
	deadline := time.Now().Add(5 * time.Second)
	for {
		apiOK(t, dir, api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &list)
		if len(list.Decisions) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ask missing")
		}
		time.Sleep(time.Millisecond)
	}
	id := list.Decisions[0].DecisionID
	stopServer(t, dir, done)
	done = make(chan int, 1)
	go func() {
		code, err := Run("test", "test")
		if err != nil {
			t.Error(err)
		}
		done <- code
	}()
	ready = dialClient(t, dir)
	ready.conn.Close()
	defer stopServer(t, dir, done)
	apiOK(t, dir, api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &list)
	if len(list.Decisions) != 1 || list.Decisions[0].DecisionID != id {
		t.Fatal(list)
	}
	yes := "yes"
	apiOK(t, dir, api.MethodDecisionAnswer, api.DecisionAnswerParams{DecisionID: id, OptionID: &yes}, nil)
	select {
	case code := <-exit:
		if code != 0 || !bytes.Contains(out.Bytes(), []byte(id)) {
			t.Fatalf("code %d out %s err %s", code, &out, &errs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ask reconnect failed")
	}
}
func TestDecisionWatchRealServerStreamAndPipeClose(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	ready := dialClient(t, dir)
	ready.conn.Close()
	defer stopServer(t, dir, done)
	initial := createDecision(t, dir, nil)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	exit := make(chan int, 1)
	go func() {
		exit <- cli.Run([]string{"decision", "watch"}, cli.Options{Session: "test", Explicit: true, Stdout: writer, Stderr: &bytes.Buffer{}})
	}()
	rd := bufio.NewReader(reader)
	row := func() map[string]json.RawMessage {
		reader.SetReadDeadline(time.Now().Add(5 * time.Second))
		line, err := rd.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]json.RawMessage
		if json.Unmarshal(line, &value) != nil {
			t.Fatal(string(line))
		}
		var owner map[string]string
		json.Unmarshal(value["machine"], &owner)
		if owner["id"] != "local" || owner["label"] != "Local" {
			t.Fatal(string(line))
		}
		return value
	}
	if string(row()["event"]) != `"machine_up"` {
		t.Fatal("up missing")
	}
	first := row()
	var d api.Decision
	json.Unmarshal(first["decision"], &d)
	if string(first["event"]) != `"created"` || d.DecisionID != initial.DecisionID {
		t.Fatal(first)
	}
	next := createDecision(t, dir, nil)
	created := row()
	json.Unmarshal(created["decision"], &d)
	if string(created["event"]) != `"created"` || d.DecisionID != next.DecisionID || len(d.Options) != 1 {
		t.Fatal(created)
	}
	yes := "yes"
	apiOK(t, dir, api.MethodDecisionAnswer, api.DecisionAnswerParams{DecisionID: next.DecisionID, OptionID: &yes}, nil)
	resolved := row()
	json.Unmarshal(resolved["decision"], &d)
	if string(resolved["event"]) != `"resolved"` || d.Status != "answered" || d.Answer == nil {
		t.Fatal(resolved)
	}
	reader.Close()
	select {
	case code := <-exit:
		if code != 0 {
			t.Fatal(code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pipe close did not end watch")
	}
}
func TestDecisionWatchReconnectSnapshot(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	ready := dialClient(t, dir)
	ready.conn.Close()
	pending := createDecision(t, dir, nil)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	exit := make(chan int, 1)
	go func() {
		exit <- cli.Run([]string{"decision", "watch"}, cli.Options{Session: "test", Explicit: true, Stdout: writer, Stderr: &bytes.Buffer{}})
	}()
	rd := bufio.NewReader(reader)
	row := func() map[string]json.RawMessage {
		reader.SetReadDeadline(time.Now().Add(5 * time.Second))
		line, err := rd.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]json.RawMessage
		if json.Unmarshal(line, &v) != nil {
			t.Fatal(string(line))
		}
		return v
	}
	if string(row()["event"]) != `"machine_up"` || string(row()["event"]) != `"created"` {
		t.Fatal("initial sync")
	}
	stopServer(t, dir, done)
	if value := row(); string(value["event"]) != `"machine_down"` || len(value["reason"]) == 0 {
		t.Fatal(value)
	}
	done = make(chan int, 1)
	go func() {
		code, err := Run("test", "test")
		if err != nil {
			t.Error(err)
		}
		done <- code
	}()
	ready = dialClient(t, dir)
	ready.conn.Close()
	defer stopServer(t, dir, done)
	for {
		value := row()
		if string(value["event"]) == `"machine_up"` {
			break
		}
		if string(value["event"]) != `"machine_down"` {
			t.Fatal(value)
		}
	}
	value := row()
	var d api.Decision
	json.Unmarshal(value["decision"], &d)
	if string(value["event"]) != `"created"` || d.DecisionID != pending.DecisionID {
		t.Fatal(value)
	}
	reader.Close()
	select {
	case code := <-exit:
		if code != 0 {
			t.Fatal(code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch survived pipe close")
	}
}
