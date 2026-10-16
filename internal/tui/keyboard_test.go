package tui

import (
	"bytes"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestFixedThemeStillFramesHostColorReply(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.sidebarConfig.Theme.AutoSwitch = false
	a.sidebarConfig.Theme.Name = "tokyo-night"
	var out bytes.Buffer
	a.out = &out
	a.BeginHostTheme()
	if !a.fr.ColorReportPending || !bytes.Contains(out.Bytes(), []byte("\x1b]11;?\x1b\\")) {
		t.Fatal("fixed theme did not request and frame host color")
	}
	specFeed(a, "\x1b]11;rgb:ffff/ffff/ffff\x1b\\")
	if a.fr.ColorReportPending || a.sidebarConfig.Theme.Name != "tokyo-night" {
		t.Fatal("host reply leaked or changed manual theme")
	}
}

func TestHostKeyboardTracksFocusedPaneRequests(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.serverUI = true
	p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", "printf READY; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	var out bytes.Buffer
	a.out = &out
	for _, step := range []struct{ request, want string }{
		{"\x1b[>8u", "\x1b[<1u\x1b[>31u"},
		{"\x1b[<u", "\x1b[<1u\x1b[>7u"},
		{"\x1b[>3u", ""}, {"\x1b[>4;1m", "\x1b[<1u\x1b[>31u"},
		{"\x1b[>4;0m", "\x1b[<1u\x1b[>7u"},
	} {
		p.WithEmu(func(e *vt.Emulator) { e.WriteString(step.request) })
		a.syncHostKeyboard()
		if out.String() != step.want {
			t.Fatalf("%q output=%q want=%q", step.request, out.String(), step.want)
		}
		out.Reset()
	}
}
