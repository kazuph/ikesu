package server

import (
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/teststate"
	sqlite3 "github.com/ncruces/go-sqlite3"
)

// Reading decisions never holds the app loop while the store is busy
// (2026-11-09: the app loop stalled up to 115s inside store.Decisions).
// Another process holds the SQLite write lock; a store write then waits for
// it while holding the store, and a decision.list waits behind that write.
// Meanwhile the app loop must keep serving other work.
func TestDecisionListDoesNotHoldAppLoopWhileStoreIsBusy(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "decision-read-offloop")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	defer func() { s.app.Stop(); <-appDone }()
	other, err := sqlite3.Open(dispatch.DBPath(s.session))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = other.Exec("COMMIT")
		}
	}
	defer release()
	writing := make(chan struct{})
	writeDone := make(chan struct{})
	go func() {
		close(writing)
		_, _ = store.CreateDecision(api.DecisionCreateParams{Kind: "agent_prompt", Title: "busy", Options: []api.DecisionOption{{ID: "1", Label: "one", Role: "other"}}}, time.Now())
		close(writeDone)
	}()
	<-writing
	// Wait until the write really holds the store: a probe read stays blocked.
	for held := false; !held; {
		probe := make(chan struct{})
		go func() { _, _ = store.Decisions("pending"); close(probe) }()
		select {
		case <-probe:
		case <-writeDone:
			t.Fatal("store write finished while another connection held the write lock")
		case <-time.After(100 * time.Millisecond):
			held = true
		}
	}
	listDone := make(chan api.Response, 1)
	go func() {
		listDone <- s.dispatch(&api.Request{ID: "list", Method: api.MethodDecisionList, Params: api.MarshalParams(api.DecisionListParams{})})
	}()
	// The app loop keeps answering while the list waits for the store. Ask
	// repeatedly so at least one request comes after the list was dispatched.
	for i := 0; i < 30; i++ {
		answered := make(chan struct{})
		go func() { s.app.Do(func() {}); close(answered) }()
		select {
		case <-answered:
		case <-time.After(2 * time.Second):
			release()
			t.Fatal("app loop was held by decision.list while the store was busy")
		}
		select {
		case <-listDone:
			t.Fatal("list finished before the store was released; the store was not busy")
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	release()
	<-writeDone
	if resp := <-listDone; resp.Err != nil {
		t.Fatalf("list failed: %+v", resp.Err)
	}
}
