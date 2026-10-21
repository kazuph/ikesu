package server

import (
	"testing"
	"time"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestRetiredServerRejectsCancelMonitor(t *testing.T){
	teststate.Lock(t)
	s:=newTestServer(t,"retired-monitor")
	store,err:=s.db();if err!=nil{t.Fatal(err)}
	s.closing.Store(2)
	if err:=store.Close();err!=nil{t.Fatal(err)}
	s.resumeJobCancel("j0001",jobRunnerRecord{PID:1})
	deadline:=time.Now().Add(time.Second)
	for {
		s.jobProcs.mu.Lock();active:=s.jobProcs.cancelling["j0001"];s.jobProcs.mu.Unlock()
		if !active{return}
		if time.Now().After(deadline){t.Fatal("retired server admitted monitor")}
		time.Sleep(time.Millisecond)
	}
}
