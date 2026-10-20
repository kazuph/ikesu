package server

import (
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestDecisionWaitAfterAppStopClosesWithoutInvalidResponse(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "stopped-wait")
	s.initDecisionHub()
	s.app.SetQuitHook(func() { s.stopDecisions(); s.closing.Store(1) })
	done := make(chan struct{})
	go func() { s.app.Run(); close(done) }()
	s.app.Stop()
	<-done
	defer s.sup.KillAll()
	path := filepath.Join(teststate.TempDir(t), "wait.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	peer, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan struct{})
	go func() {
		defer close(served)
		defer peer.Close()
		s.serveDecisionWait(peer, &api.Request{ID: "wait", Method: api.MethodDecisionWait, Params: api.MarshalParams(api.DecisionWaitParams{DecisionID: "persisted"})})
	}()
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(client)
	if err != nil || len(data) != 0 {
		t.Fatalf("stopped wait must close for reconnect, response=%s err=%v", data, err)
	}
	<-served
}
