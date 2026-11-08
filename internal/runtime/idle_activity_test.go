package runtime

import (
	"github.com/charmbracelet/x/vt"
	"testing"
	"time"
)

func TestIdleActivityRecordsOnlyKeyInput(t *testing.T) {
	p := NewSupervisor().newPane(1, t.TempDir(), 20, 10)
	before, seq := p.IdleActivity()
	if before.IsZero() || seq != 0 {
		t.Fatalf("initial activity = %v, %d", before, seq)
	}
	p.RecordExternalInput()
	p.WithEmu(func(e *vt.Emulator) { _, _ = e.Write([]byte("terminal output")) })
	after, n := p.IdleActivity()
	if !after.Equal(before) || n != seq {
		t.Fatalf("output or automation changed activity: %v %d", after, n)
	}
	p.RecordKeyInput()
	after, n = p.IdleActivity()
	if after.Before(before) || n != seq+1 {
		t.Fatalf("key activity = %v, %d", after, n)
	}
	p.RecordWorkingActivity()
	last, n := p.IdleActivity()
	if last.Before(after) || n != seq+2 {
		t.Fatalf("working activity = %v, %d", last, n)
	}
}

func TestIdleActivitySurvivesLiveHandoff(t *testing.T) {
	s, next := NewSupervisor(), NewSupervisor()
	defer s.KillAll()
	defer next.KillAll()
	p, err := s.SpawnWith(1, t.TempDir(), 20, 10, nil, []string{"/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	p.RecordKeyInput()
	at, seq := p.IdleActivity()
	states, files, err := s.PauseHandoff()
	if err != nil {
		t.Fatal(err)
	}
	if err := next.ImportHandoff(states, files); err != nil {
		t.Fatal(err)
	}
	s.ReleaseHandoff()
	next.StartImported()
	got, gotSeq := next.Get(1).IdleActivity()
	if !got.Equal(at) || gotSeq != seq {
		t.Fatalf("handoff activity = %v, %d; want %v, %d", got, gotSeq, at, seq)
	}
}

func TestIdleActivityRestoreDefaultsToNow(t *testing.T) {
	p := NewSupervisor().newPane(1, t.TempDir(), 20, 10)
	start := time.Now()
	p.RestoreIdleActivity(time.Time{}, 0)
	at, seq := p.IdleActivity()
	if at.Before(start) || seq != 0 {
		t.Fatalf("old pane activity = %v, %d", at, seq)
	}
	old := start.Add(-time.Hour)
	p.RestoreIdleActivity(old, 7)
	at, seq = p.IdleActivity()
	if !at.Equal(old) || seq != 7 {
		t.Fatalf("restored activity = %v, %d", at, seq)
	}
}
