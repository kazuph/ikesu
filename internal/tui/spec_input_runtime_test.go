package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/input"
)

func specWaitFile(t *testing.T, path string, want []byte) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		b, err := os.ReadFile(path)
		if err == nil && len(b) >= len(want) {
			if string(b) != string(want) {
				t.Fatalf("PTY bytes=%q want=%q", b, want)
			}
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("PTY capture timeout: %s", path)
		case <-tick.C:
		}
	}
}

func specFeed(a *App, raw string) {
	for _, ev := range a.fr.Push([]byte(raw)) {
		a.onInput(ev)
	}
}

func TestSPECRawLFShiftEnterAndFixTermActualPTYBytes(t *testing.T) {
	for _, tc := range []struct{ name, output, host, want string }{
		{"rawLF", "", "\n", "\n"},
		{"legacyShiftEnter", "", "\x1b[27;2;13~", "\n"},
		{"legacyReleaseDropped", "", "\x1b[13;2:3uZ", "Z"},
		{"FixTermQuery", "\x1b[?u", "", "\x1b[?0u"},
		{"FixTermLegacyLF", "\x1b[?u", "\x1b[27;2;13~", "\x1b[?0u\n"},
		{"ExplicitKittyKeepsProtocol", "\x1b[>1u", "\x1b[27;2;13~", "\x1b[27;2;13~"},
		{"EmptyModifierA", "", "\x1b[97;;97u", "\x1b[97;;97u"},
		{"ShiftedAssociatedA", "", "\x1b[97:65;2;65u", "\x1b[97:65;2;65u"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := specRuntimeApp(t)
			capture := filepath.Join(t.TempDir(), "bytes")
			// Real raw-mode child records every byte received through the PTY.
			script := fmt.Sprintf("stty raw -echo; printf '%%sREADY' \"$1\"; dd bs=1 count=%d of=\"$2\" 2>/dev/null; exec sleep 60", len(tc.want))
			p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", script, "capture", tc.output, capture})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "READY")
			specFeed(a, tc.host)
			specWaitFile(t, capture, []byte(tc.want))
		})
	}
}

func TestSPECNonterminalLFUsesCtrlJ(t *testing.T) {
	a := specRuntimeApp(t)
	a.openHelp()
	specFeed(a, "\n")
	if a.help == nil || a.help.scroll != 1 {
		t.Fatal("raw LF did not follow help Ctrl+j path")
	}
	var f input.Framer
	events := f.Push([]byte{'\n'})
	if len(events) != 1 || events[0].Key.Code != 'j' {
		t.Fatal("LF decoder", events)
	}
}

func TestSPECRealChildEnvironmentAndNestedColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	a := specRuntimeApp(t)
	p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", `printf 'ENV:%s:%s:%s\r\n' "${NO_COLOR-unset}" "$TERM" "$COLORTERM"; if [ -z "${NO_COLOR+x}" ]; then printf '\033[31mCOLOR_RED\033[0m\r\n'; else printf MONOCHROME; fi; exec sleep 60`})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "COLOR_RED")
	text := p.ScreenText()
	if !strings.Contains(text, "ENV:unset:xterm-256color:truecolor") || strings.Contains(text, "MONOCHROME") {
		t.Fatal("real child env", text)
	}
	colored := false
	for _, row := range p.BufferLines() {
		for _, cell := range row {
			if cell.Content == "C" && cell.Style.Fg != nil {
				colored = true
			}
		}
	}
	if !colored {
		t.Fatal("nested TTY color stripped")
	}
}
