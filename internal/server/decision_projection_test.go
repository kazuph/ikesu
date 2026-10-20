package server

import (
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
	"time"
)

func projectionReader(c *testConn) <-chan *endpoint.DecisionProjection {
	ch := make(chan *endpoint.DecisionProjection, 16)
	go func() {
		defer close(ch)
		for {
			line, err := c.rd.ReadBytes('\n')
			if err != nil {
				return
			}
			var m clientMsg
			if json.Unmarshal(line, &m) != nil {
				return
			}
			if m.Type == "decisions" && m.Decisions != nil {
				ch <- m.Decisions
			}
		}
	}()
	return ch
}
func readProjection(t *testing.T, ch <-chan *endpoint.DecisionProjection, count int) *endpoint.DecisionProjection {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case p, ok := <-ch:
			if !ok {
				t.Fatal("projection connection closed")
			}
			if len(p.Pending) == count {
				return p
			}
		case <-timer.C:
			t.Fatal("projection missing", count)
		}
	}
}
func TestDecisionProjectionHiddenClientAnswerAndExternalResolve(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	a := dialClient(t, dir)
	b := dialClient(t, dir)
	defer a.conn.Close()
	defer b.conn.Close()
	ach, bch := projectionReader(a), projectionReader(b)
	a.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	initial := readProjection(t, ach, 0)
	hidden := false
	b.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24, Visible: &hidden})
	second := readProjection(t, bch, 0)
	if initial.BootID == "" || initial.BootID != second.BootID || initial.Revision != second.Revision {
		t.Fatal(initial, second)
	}
	var created api.DecisionResult
	apiOK(t, dir, api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "ask", Title: "UI", AllowText: true, Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}}, &created)
	pa, pb := readProjection(t, ach, 1), readProjection(t, bch, 1)
	if pa.Revision <= initial.Revision || pa.Revision != pb.Revision || pb.Pending[0].DecisionID != created.Decision.DecisionID {
		t.Fatal(pa, pb)
	}
	yes, text := "yes", "client text"
	b.send(t, clientMsg{Type: "decision_answer", DecisionAnswer: &api.DecisionAnswerParams{DecisionID: created.Decision.DecisionID, OptionID: &yes, Text: &text, Responder: "forged"}})
	readProjection(t, ach, 0)
	readProjection(t, bch, 0)
	var result api.DecisionResult
	apiOK(t, dir, api.MethodDecisionGet, api.DecisionTargetParams{DecisionID: created.Decision.DecisionID}, &result)
	if result.Decision.Answer == nil || result.Decision.Answer.Responder != "client" || *result.Decision.Answer.Text != text {
		t.Fatal(result)
	}
	d := createDecision(t, dir, nil)
	readProjection(t, ach, 1)
	readProjection(t, bch, 1)
	apiOK(t, dir, api.MethodDecisionCancel, api.DecisionTargetParams{DecisionID: d.DecisionID}, nil)
	readProjection(t, ach, 0)
	readProjection(t, bch, 0)
	ms := uint64(100)
	createDecision(t, dir, &ms)
	readProjection(t, ach, 1)
	readProjection(t, bch, 1)
	readProjection(t, ach, 0)
	readProjection(t, bch, 0)
}
