package dispatch

import (
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "gtx.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func i64(v int64) *int64 { return &v }

func TestInsertUnreadAndDeliverMarksInOrder(t *testing.T) {
	s := testStore(t)
	if _, err := s.InsertMessage(DefaultRoom, "/repo", "alice", "bob", "one", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertMessage(DefaultRoom, "/repo", "alice", "bob", "two", nil, false); err != nil {
		t.Fatal(err)
	}

	unread, err := s.MessagesForInboxRecipients(DefaultRoom, []string{"bob"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 2 || unread[0].Body != "one" || unread[1].Body != "two" {
		t.Fatalf("unexpected inbox order: %+v", unread)
	}
	for _, m := range unread {
		if m.DeliveredAt == nil || m.ReadAt == nil {
			t.Fatalf("inbox message should be delivered+read: %+v", m)
		}
	}
	after, err := s.MessagesForInboxRecipients(DefaultRoom, []string{"bob"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("inbox should be empty after read: %+v", after)
	}
}

func TestInFlightMessagesStayQueued(t *testing.T) {
	s := testStore(t)
	m, _ := s.InsertMessage(DefaultRoom, "", "alice", "bob", "pending", nil, false)
	got, err := s.MessagesForInboxRecipients(DefaultRoom, []string{"bob"}, map[int64]bool{m.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("in-flight message must not be marked read")
	}
	pend, _ := s.PendingMessagesForAgent(DefaultRoom, "bob")
	if len(pend) != 1 {
		t.Fatal("in-flight message must stay queued")
	}
}

func TestMarkMessageDeliveredIsIdempotent(t *testing.T) {
	s := testStore(t)
	m, _ := s.InsertMessage(DefaultRoom, "", "alice", "bob", "hi", nil, false)
	ok, err := s.MarkMessageDelivered(m.ID)
	if err != nil || !ok {
		t.Fatalf("first delivery must succeed: %v %v", ok, err)
	}
	ok, err = s.MarkMessageDelivered(m.ID)
	if err != nil || ok {
		t.Fatalf("second delivery must be a no-op: %v %v", ok, err)
	}
}

func TestReplyToPreservesReadAtAndSetsRepliedAt(t *testing.T) {
	s := testStore(t)
	orig, _ := s.InsertMessage(DefaultRoom, "", "alice", "bob", "question", nil, false)
	if _, err := s.MarkDelivered(DefaultRoom, "bob"); err != nil {
		t.Fatal(err)
	}
	replyTo := orig.ID
	if _, err := s.InsertMessage(DefaultRoom, "", "bob", "alice", "answer", &replyTo, false); err != nil {
		t.Fatal(err)
	}
	hist, err := s.History(DefaultRoom, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	var again *Message
	for i := range hist {
		if hist[i].ID == orig.ID {
			again = &hist[i]
		}
	}
	if again == nil || again.ReadAt == nil {
		t.Fatalf("reply must not clear the original's read_at: %+v", hist)
	}
}

func TestHistoryFiltersRoomAndProject(t *testing.T) {
	s := testStore(t)
	s.InsertMessage("room-a", "/repo-a", "alice", "bob", "a", nil, false)
	s.InsertMessage("room-a", "/repo-b", "alice", "bob", "b", nil, false)
	s.InsertMessage("room-b", "/repo-a", "alice", "bob", "c", nil, false)

	proj := "/repo-b"
	msgs, err := s.History("room-a", &proj, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Body != "b" {
		t.Fatalf("unexpected history: %+v", msgs)
	}
	rooms, err := s.Rooms()
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 2 || rooms[0] != "room-a" || rooms[1] != "room-b" {
		t.Fatalf("unexpected rooms: %v", rooms)
	}
}

func TestPendingNudgesGroupByRoom(t *testing.T) {
	s := testStore(t)
	s.InsertMessage("room-b", "", "alice", "bob", "b1", nil, false)
	s.InsertMessage("room-a", "", "alice", "bob", "a1", nil, false)
	s.InsertMessage("room-b", "", "carol", "bob", "b2", nil, false)
	s.InsertMessage("room-a", "", "alice", "carol", "ignored", nil, false)

	nudges, err := s.PendingNudgesForAgent("bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(nudges) != 2 {
		t.Fatalf("expected 2 rooms of nudges: %+v", nudges)
	}
	if nudges[0].Room != "room-b" || nudges[0].Count != 2 || nudges[0].LatestFrom != "carol" {
		t.Fatalf("unexpected nudge: %+v", nudges[0])
	}
	if nudges[1].Room != "room-a" || nudges[1].Count != 1 || nudges[1].LatestFrom != "alice" {
		t.Fatalf("unexpected nudge: %+v", nudges[1])
	}
}

func TestJobLifecycleAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gtx.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertCommand("job-1", "tests", "'go' 'test'", "/repo", "p1", "alpha", "summary", "/tmp/job-1.log"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCommandRunning("job-1", 1234); err != nil {
		t.Fatal(err)
	}
	s.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	job, err := reopened.CommandRow("job-1")
	if err != nil || job == nil {
		t.Fatalf("job not found: %v", err)
	}
	if job.Status != "running" || job.RunnerPID == nil || *job.RunnerPID != 1234 {
		t.Fatalf("unexpected job: %+v", job)
	}
	if job.StartedMs == nil {
		t.Fatal("started_unix_ms missing")
	}
	if ok, err := reopened.MarkCommandFinished("job-1", i64(7)); err != nil || !ok {
		t.Fatalf("finish failed: %v %v", ok, err)
	}
	job, _ = reopened.CommandRow("job-1")
	if job.Status != "exited" || job.ExitCode == nil || *job.ExitCode != 7 {
		t.Fatalf("unexpected exited job: %+v", job)
	}
}

func TestJobTerminalTransitionSingleOwner(t *testing.T) {
	s := testStore(t)
	s.InsertCommand("cancel-wins", "t", "c", "/r", "p1", "a", "summary", "/l")
	s.MarkCommandRunning("cancel-wins", 10)
	if ok, _ := s.MarkCommandCancelling("cancel-wins"); !ok {
		t.Fatal("cancelling failed")
	}
	if ok, _ := s.MarkCommandFinished("cancel-wins", i64(0)); ok {
		t.Fatal("finish must not win after cancelling")
	}
	if ok, _ := s.MarkCommandCancelled("cancel-wins"); !ok {
		t.Fatal("cancel failed")
	}
	if ok, _ := s.MarkCommandFinished("cancel-wins", i64(0)); ok {
		t.Fatal("finish must not win after cancelled")
	}

	s.InsertCommand("finish-wins", "t", "c", "/r", "p1", "a", "summary", "/l")
	s.MarkCommandRunning("finish-wins", 11)
	if ok, _ := s.MarkCommandFinished("finish-wins", i64(0)); !ok {
		t.Fatal("finish failed")
	}
	if ok, _ := s.MarkCommandCancelling("finish-wins"); ok {
		t.Fatal("cancel must not win after exited")
	}
	if ok, _ := s.MarkCommandCancelled("finish-wins"); ok {
		t.Fatal("cancel must not win after exited")
	}
}

func TestReplyToCommandSetsRepliedAtWithoutStatusChange(t *testing.T) {
	s := testStore(t)
	s.InsertCommand("job-x", "t", "c", "/r", "p1", "alpha", "summary", "/l")
	s.MarkCommandRunning("job-x", 1)
	if ok, _ := s.MarkCommandFinished("job-x", i64(0)); !ok {
		t.Fatal("finish failed")
	}
	did, err := s.CommandDispatchID("job-x")
	if err != nil || did == nil {
		t.Fatalf("dispatch id missing: %v", err)
	}
	if _, err := s.InsertMessage(JobsRoom, "/r", "gtx-run", "alpha", "done", did, false); err != nil {
		t.Fatal(err)
	}
	job, _ := s.CommandRow("job-x")
	if job.Status != "exited" {
		t.Fatalf("reply must not change job status: %+v", job)
	}
}

// The gtx-jobs batch writer must mark only the message ids it actually
// wrote: a completion that lands between payload assembly and the PTY
// write finishing would otherwise be marked delivered without ever
// reaching the pane.
func TestMarkDeliveredByIDsLeavesNewArrivalsQueued(t *testing.T) {
	s := testStore(t)
	first, err := s.InsertMessage(JobsRoom, "", "gtx-run", "bob", "done-1", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	// Payload assembly snapshots the queued ids...
	pending, err := s.PendingMessagesForAgent(JobsRoom, "bob")
	if err != nil || len(pending) != 1 || pending[0].ID != first.ID {
		t.Fatalf("assemble: %+v err=%v", pending, err)
	}
	// ...then a second completion lands before the write completes.
	second, err := s.InsertMessage(JobsRoom, "", "gtx-run", "bob", "done-2", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{pending[0].ID}
	if n, err := s.MarkDeliveredByIDs(ids); err != nil || n != 1 {
		t.Fatalf("MarkDeliveredByIDs=%d err=%v", n, err)
	}
	rest, err := s.PendingMessagesForAgent(JobsRoom, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 1 || rest[0].ID != second.ID {
		t.Fatalf("late arrival must stay queued: %+v", rest)
	}
	if n, _ := s.MarkDeliveredByIDs([]int64{second.ID}); n != 1 {
		t.Fatalf("second mark=%d", n)
	}
	if rest, _ := s.PendingMessagesForAgent(JobsRoom, "bob"); len(rest) != 0 {
		t.Fatalf("all delivered now: %+v", rest)
	}
}
