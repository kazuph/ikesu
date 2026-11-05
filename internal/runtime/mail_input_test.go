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
