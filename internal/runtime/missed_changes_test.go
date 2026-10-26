package runtime

import "testing"

func pendingChangePane(t *testing.T) (*Supervisor, *Pane) {
	t.Helper()
	s := NewSupervisor()
	p := s.newPane(1, t.TempDir(), 80, 24)
	s.panes[p.ID] = p
	t.Cleanup(func() { p.emu.Close() })
	return s, p
}

func TestPendingChangesRecoverLastOutputFromFullRuntimeQueue(t *testing.T) {
	s, p := pendingChangePane(t)
	for i := 0; i < cap(s.events); i++ {
		p.notify(EventStatus)
	}
	p.notifyChanged()
	p.notifyChanged()
	events := s.TakePendingChanges()
	if len(events) != 1 || events[0].Pane != p.ID || events[0].Kind != EventChanged {
		t.Fatalf("last output was not recoverable: %+v", events)
	}
	p.MarkChangedRead()
	if events = s.TakePendingChanges(); len(events) != 0 {
		t.Fatal("acknowledged change repeated", events)
	}
	for len(s.events) > 0 {
		<-s.events
	}
	p.notifyChanged()
	select {
	case event := <-s.Events():
		if event.Kind != EventChanged || event.Pane != p.ID {
			t.Fatal(event)
		}
	default:
		t.Fatal("new output after recovery was not delivered")
	}
}

func TestPendingExitSurvivesAcknowledgingEarlierChanged(t *testing.T) {
	s, p := pendingChangePane(t)
	p.notifyChanged()
	for len(s.events) < cap(s.events) {
		p.notify(EventStatus)
	}
	p.notify(EventExited)
	if event := <-s.Events(); event.Kind != EventChanged {
		t.Fatal(event)
	}
	p.MarkChangedRead()
	events := s.TakePendingChanges()
	if len(events) != 1 || events[0].Kind != EventExited {
		t.Fatalf("earlier output acknowledgement lost the exit: %+v", events)
	}
}

func TestPendingChangesDoNotApplyToReusedPaneID(t *testing.T) {
	s, p := pendingChangePane(t)
	for i := 0; i < cap(s.events); i++ {
		p.notify(EventStatus)
	}
	p.notifyChanged()
	replacement := s.newPane(p.ID, t.TempDir(), 80, 24)
	t.Cleanup(func() { replacement.emu.Close() })
	s.panes[p.ID] = replacement
	if events := s.TakePendingChanges(); len(events) != 0 {
		t.Fatal("old pane's notification reached its replacement", events)
	}
}
