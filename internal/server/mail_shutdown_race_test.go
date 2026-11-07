package server

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestMsgSendReadFinishesBeforeStoreClose(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, dispatch.DBPathEnvVar, filepath.Join(t.TempDir(), "mail.db"))
	s := newTestServer(t, "mail-shutdown-race")
	s.mailCh = make(chan mailWrite, 4)
	s.mailInFlight = map[int64]bool{}
	s.jobInFlight = map[string]bool{}
	s.app.SetReported(1, "codex", "working")
	var err error
	s.apiLn, err = net.Listen("unix", filepath.Join(teststate.TempDir(t), "api.sock"))
	if err != nil {
		t.Fatal(err)
	}
	s.clientLn, err = net.Listen("unix", filepath.Join(teststate.TempDir(t), "client.sock"))
	if err != nil {
		t.Fatal(err)
	}
	s.lock, err = os.CreateTemp(t.TempDir(), "session-lock")
	if err != nil {
		t.Fatal(err)
	}
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertCommand("lock", "lock", "true", t.TempDir(), "p1", "p1", "none", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCommandRunning("lock", int64(os.Getpid())); err != nil {
		t.Fatal(err)
	}

	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	client, err := net.Dial("unix", s.apiLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	peer, err := s.apiLn.Accept()
	if err != nil {
		t.Fatal(err)
	}
	apiDone := make(chan any, 1)
	s.apiWG.Add(1)
	go func() {
		defer func() { apiDone <- recover() }()
		s.serveAPI(peer)
	}()
	s.mailMu.Lock()
	if err := api.WriteRequest(client, api.Request{ID: "send", Method: api.MethodMsgSend, Params: api.MarshalParams(api.MsgSendParams{Room: "room", FromAgent: "sender", To: "p1", Body: "body"})}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		messages, err := store.History("room", nil, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("msg.send did not insert")
		}
		runtime.Gosched()
	}
	locked, release := make(chan struct{}), make(chan struct{})
	lockDone := make(chan any, 1)
	logDir := t.TempDir()
	go func() {
		defer func() {
			if p := recover(); p != nil {
				lockDone <- p
			}
		}()
		lockDone <- store.CleanupJobs(time.Now(), logDir, func(pid int) bool { close(locked); <-release; return processGroupAlive(pid) })
	}()
	<-locked
	s.mailMu.Unlock()
	deadline = time.Now().Add(5 * time.Second)
	stack := make([]byte, 1<<20)
	for {
		n := runtime.Stack(stack, true)
		if bytes.Contains(stack[:n], []byte("internal/dispatch.(*Store).PendingForAgentCreationOrder")) {
			break
		}
		if time.Now().After(deadline) {
			t.Error("msg.send did not reach pending read")
			break
		}
		runtime.Gosched()
	}
	s.app.Stop()
	<-appDone
	cleanupDone := make(chan any, 1)
	go func() { defer func() { cleanupDone <- recover() }(); s.cleanup() }()
	// cleanup historically waits 500ms for API requests before closing Store.
	select {
	case p := <-cleanupDone:
		t.Errorf("cleanup finished before the read was released: %v", p)
		cleanupDone = nil
	case <-time.After(time.Second):
	}
	close(release)
	if result := <-lockDone; result != nil {
		t.Errorf("database operation: %v", result)
	}
	if cleanupDone != nil {
		if p := <-cleanupDone; p != nil {
			t.Errorf("cleanup panic: %v", p)
		}
	}
	if s.beginMailQuery() {
		s.mailQueryWG.Done()
		t.Error("read accepted after shutdown")
	}
	_ = client.Close()
	if p := <-apiDone; p != nil {
		t.Errorf("API panic: %v", p)
	}
}
