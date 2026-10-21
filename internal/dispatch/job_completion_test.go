package dispatch

import (
	"sync"
	"testing"
	"time"
)

func TestJobCompletionAndNoticeAreAtomic(t *testing.T) {
	s := testStore(t)
	if err := s.InsertCommand("j0001", "handoff", "false", "/", "p1", "codex", "summary", "/tmp/log"); err != nil { t.Fatal(err) }
	if err := s.MarkCommandRunning("j0001", 123); err != nil { t.Fatal(err) }
	// A real SQLite trigger fails precisely at the notification insert.
	if err := s.db.Exec(`CREATE TRIGGER reject_notice BEFORE INSERT ON dispatches WHEN NEW.kind='message' BEGIN SELECT RAISE(ABORT,'notice failure'); END`); err != nil { t.Fatal(err) }
	finished := time.Now().UnixMilli()
	exit := int64(7)
	if _,err:=s.CompleteJob("j0001","running","exited",&exit,finished,"p1","notice");err==nil{t.Fatal("insert failure accepted")}
	row,err:=s.CommandRow("j0001");if err!=nil || row.Status!="running" || row.FinishedMs!=nil{t.Fatal("finish was not rolled back",row,err)}
	if err:=s.db.Exec(`DROP TRIGGER reject_notice`);err!=nil{t.Fatal(err)}
	var wg sync.WaitGroup
	for i:=0;i<8;i++{wg.Go(func(){if _,err:=s.CompleteJob("j0001","running","exited",&exit,finished,"p1","notice");err!=nil{t.Error(err)}})}
	wg.Wait()
	row,err=s.CommandRow("j0001");if err!=nil || row.ExitCode==nil || *row.ExitCode!=7 || row.FinishedMs==nil || *row.FinishedMs!=finished{t.Fatal(row,err)}
	messages,err:=s.History(JobsRoom,nil,0);if err!=nil{t.Fatal(err)}
	count:=0;for _,m:=range messages{if m.Body=="notice"{count++}}
	if count!=1{t.Fatalf("completion notices=%d",count)}
}
