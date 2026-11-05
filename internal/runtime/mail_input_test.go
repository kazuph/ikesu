package runtime

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

func TestMailWaitsForVisibleAgentDraft(t *testing.T) {
	cases := []struct {
		agent, screen string
		want          bool
	}{
		{"codex", "\x1b[H\x1b[2J› \x1b[2mAsk Codex to do anything\x1b[0m", false},
		{"codex", "\x1b[H\x1b[2J› draft sample", true},
		{"codex", "\x1b[H\x1b[2J\x1b[48;2;30;30;30m›\x1b[0m\r\n\r\n\x1b[48;2;30;30;30m  lastline\x1b[0m\r\n\r\n  GPT footer", true},
		{"claude", "\x1b[H\x1b[2J❯ \x1b[7mT\x1b[0;2mry a prompt\x1b[0m\r\n──────", false},
		{"claude", "\x1b[H\x1b[2J❯ draft sample\r\n──────", true},
		{"claude", "\x1b[H\x1b[2J❯ \x1b[7mx\x1b[0m\r\n──────", true},
		{"other", "\x1b[H\x1b[2J❯ draft sample", false},
	}
	for _, tc := range cases {
		t.Run(tc.agent+"/"+tc.screen, func(t *testing.T) {
			sup := NewSupervisor()
			p, err := sup.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
			if err != nil {
				t.Fatal(err)
			}
			defer sup.Kill(1)
			p.SetAgent(tc.agent)
			p.WithEmu(func(e *vt.Emulator) { _, _ = e.Write([]byte(tc.screen)) })
			if got := p.MailInputActive(); got != tc.want {
				t.Fatalf("active=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestMailQuietIntervalIsSeparateFromScreenPromptInterval(t *testing.T) {
	sup := NewSupervisor()
	p, err := sup.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer sup.Kill(1)
	p.RecordKeyInput()
	if !p.MailInputActive() {
		t.Fatal("recent input did not defer mail")
	}
	p.lastMailExternalInput.Store(time.Now().Add(-MailInputQuiet - time.Second).UnixNano())
	if p.MailInputActive() {
		t.Fatal("quiet pane remained active")
	}
}

func TestMailQuietIntervalSurvivesLiveHandoff(t *testing.T) {
	old := NewSupervisor()
	pane, err := old.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	pane.RecordKeyInput()
	states, files, err := old.PauseHandoff()
	if err != nil {
		old.KillAll()
		t.Fatal(err)
	}
	next := NewSupervisor()
	if err := next.ImportHandoff(states, files); err != nil {
		old.ResumeHandoff()
		old.KillAll()
		t.Fatal(err)
	}
	old.ReleaseHandoff()
	next.StartImported()
	defer next.KillAll()
	if !next.Get(1).MailInputActive() {
		t.Fatal("recent input was lost during live handoff")
	}
}

func TestMailHandoffRetainsInputAfterSnapshot(t *testing.T) {
	old := NewSupervisor()
	pane, err := old.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	states, files, err := old.PauseHandoff()
	if err != nil {
		old.KillAll()
		t.Fatal(err)
	}
	// An attached client can supply input after PauseHandoff captured the pane.
	pane.RecordKeyInput()
	if _, err := pane.WriteToPTY([]byte("x")); err != nil {
		old.ResumeHandoff()
		old.KillAll()
		t.Fatal(err)
	}
	next := NewSupervisor()
	if err := next.ImportHandoff(states, files); err != nil {
		old.ResumeHandoff()
		old.KillAll()
		t.Fatal(err)
	}
	next.RestoreMailInputTimes(old.MailInputTimes())
	old.ReleaseHandoff()
	next.StartImported()
	defer next.KillAll()
	if !next.Get(1).MailInputActive() {
		t.Fatal("input after handoff snapshot was lost to the mail guard")
	}
}

func TestMailHandoffMissingTimesStartsQuietPeriod(t *testing.T) {
	sup := NewSupervisor()
	_, err := sup.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer sup.KillAll()
	sup.RestoreMailInputTimes(nil)
	if !sup.Get(1).MailInputActive() {
		t.Fatal("missing handoff time did not start quiet period")
	}
}
