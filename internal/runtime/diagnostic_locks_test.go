package runtime

import "testing"

func TestLockedPanesDoesNotWaitForPaneOrSupervisor(t *testing.T) {
	s := NewSupervisor()
	p := &Pane{ID: 7}
	s.panes[7] = p
	p.mu.Lock()
	ids, known := s.LockedPanes()
	p.mu.Unlock()
	if !known || len(ids) != 1 || ids[0] != 7 {
		t.Fatalf("held pane lock: %v %v", ids, known)
	}
	ids, known = s.LockedPanes()
	if !known || len(ids) != 0 {
		t.Fatalf("released pane lock: %v %v", ids, known)
	}
	s.mu.Lock()
	_, known = s.LockedPanes()
	s.mu.Unlock()
	if known {
		t.Fatal("busy supervisor reported a complete list")
	}
}
