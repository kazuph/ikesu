package dispatch

import (
	"reflect"
	"testing"
)

func TestReservedPaneIDsIncludesPendingJobsAndSessionLedger(t *testing.T) {
	s := testStore(t)
	m, err := s.InsertMessage("other-room", "", "sender", "p2", "pending", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertCommand("j1", "", "true", "", "p3", "p3", "summary", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.PutPaneSession(PaneSession{Session: "current", PaneID: 4}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutPaneSession(PaneSession{Session: "other", PaneID: 5}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReservedPaneIDs("current")
	if err != nil || !reflect.DeepEqual(got, map[int]bool{2: true, 3: true, 4: true}) {
		t.Fatalf("got %v, err %v", got, err)
	}
	if _, err := s.MarkMessageDelivered(m.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePaneSession("current", 4); err != nil {
		t.Fatal(err)
	}
	got, err = s.ReservedPaneIDs("current")
	if err != nil || !reflect.DeepEqual(got, map[int]bool{3: true}) {
		t.Fatalf("after delivery got %v, err %v", got, err)
	}
}

func TestJobCompletionCarriesCallerGeneration(t *testing.T) {
	s := testStore(t)
	if err := s.InsertPaneCommand("j1", "", "true", "", "p3", "p3", "summary", "", "old-generation"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCommandRunning("j1", 1); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CompleteJob("j1", "running", "exited", nil, 1, "p3", "done"); !ok || err != nil {
		t.Fatalf("complete: %v %v", ok, err)
	}
	rows, err := s.PendingMessagesForAgent(JobsRoom, "p3")
	if err != nil || len(rows) != 1 || rows[0].PaneGeneration != "old-generation" {
		t.Fatalf("notices %v, err %v", rows, err)
	}
}

func TestInboxOnlyMarksMatchingGenerationDelivered(t *testing.T) {
	s := testStore(t)
	old, err := s.InsertPaneMessage("room", "", "sender", "p1", "old", nil, false, "old")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.InsertPaneMessage("room", "", "sender", "p1", "new", nil, false, "new")
	if err != nil {
		t.Fatal(err)
	}
	gen := "new"
	rows, err := s.MessagesForInboxRecipients("room", []string{"p1"}, nil, &gen)
	if err != nil || len(rows) != 1 || rows[0].Body != "new" {
		t.Fatalf("rows %v err %v", rows, err)
	}
	rows, err = s.PendingMessagesForAgent("room", "p1")
	if err != nil || len(rows) != 1 || rows[0].ID != old.ID {
		t.Fatalf("pending %v err %v", rows, err)
	}
}
