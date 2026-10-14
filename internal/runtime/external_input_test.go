package runtime

import (
	"os"
	"testing"
)

func TestExternalInputSequenceAndScreenWriteAdmission(t *testing.T) {
	s := NewSupervisor()
	p, err := s.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.KillAll()
	if p.UserActive() {
		t.Fatal("no input falsely active")
	}
	sequence := p.ExternalInputSequence()
	if _, err := p.WriteScreenPrompt([]byte("internal\n"), &sequence); err != nil {
		t.Fatal(err)
	}
	if p.ExternalInputSequence() != sequence || p.UserActive() {
		t.Fatal("screen input counted as external")
	}
	s.Input(1, []byte("user\n"))
	if p.ExternalInputSequence() == sequence || !p.UserActive() {
		t.Fatal("external input not recorded")
	}
	if n, err := p.WriteScreenPrompt([]byte("blocked"), &sequence); err == nil || n != 0 {
		t.Fatal(n, err)
	}
	s.Kill(1)
	// No input timestamp change can make a closed descriptor writable.
	closed := &Pane{ptyFile: nil}
	if _, err := closed.WriteScreenPrompt([]byte("closed"), nil); err != os.ErrClosed {
		t.Fatal(err)
	}
}
