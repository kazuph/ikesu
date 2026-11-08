package runtime

import (
	"github.com/charmbracelet/x/vt"
	"strings"
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

func TestIdleActivityHandoffIncludesInputAfterSnapshot(t *testing.T) {
	old, next := NewSupervisor(), NewSupervisor()
	defer old.KillAll()
	defer next.KillAll()
	pane, err := old.SpawnWith(1, t.TempDir(), 20, 10, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	states, files, err := old.PauseHandoff()
	if err != nil {
		t.Fatal(err)
	}
	pane.RecordKeyInput()
	at, seq := pane.IdleActivity()
	if err := next.ImportHandoff(states, files); err != nil {
		t.Fatal(err)
	}
	next.RestoreIdleActivities(old.IdleActivities())
	old.ReleaseHandoff()
	next.StartImported()
	got, n := next.Get(1).IdleActivity()
	if !got.Equal(at) || n != seq {
		t.Fatalf("late activity = %v %d, want %v %d", got, n, at, seq)
	}
}

func TestIdleUnsentInputRecognition(t *testing.T) {
	for _, tc := range []struct {
		agent, screen  string
		pending, known bool
	}{
		{"codex", "› \x1b[2mAsk Codex to do anything\x1b[0m", false, true},
		{"codex", "› unfinished", true, true},
		{"claude", "❯ unfinished", true, true},
		{"codex", "no prompt", false, false},
		{"muse", "⟩ unfinished", false, false},
	} {
		t.Run(tc.agent+tc.screen, func(t *testing.T) {
			s := NewSupervisor()
			p, err := s.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
			if err != nil {
				t.Fatal(err)
			}
			defer s.KillAll()
			p.SetAgent(tc.agent)
			p.WithEmu(func(e *vt.Emulator) { _, _ = e.Write([]byte("\x1b[H\x1b[2J" + tc.screen)) })
			pending, known := p.UnsentAgentInput()
			if pending != tc.pending || known != tc.known {
				t.Fatalf("got %v %v want %v %v", pending, known, tc.pending, tc.known)
			}
		})
	}
}

func TestIdlePTYOutputDoesNotUpdateActivity(t *testing.T) {
	s := NewSupervisor()
	defer s.KillAll()
	p, err := s.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/sh", "-c", "printf 'IDLE_OUTPUT_PROOF'; read line"})
	if err != nil {
		t.Fatal(err)
	}
	at, seq := p.IdleActivity()
	deadline := time.After(5 * time.Second)
	for {
		lines, _ := p.ReadLines("visible", true)
		if strings.Contains(strings.Join(lines, "\n"), "IDLE_OUTPUT_PROOF") {
			break
		}
		select {
		case event := <-s.Events():
			if event.Kind == EventChanged {
				p.MarkChangedRead()
			}
		case <-deadline:
			t.Fatal("PTY output not observed")
		}
	}
	got, n := p.IdleActivity()
	if !got.Equal(at) || n != seq {
		t.Fatalf("PTY output changed activity: %v %d", got, n)
	}
}
