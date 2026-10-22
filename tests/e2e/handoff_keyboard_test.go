package e2e

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveHandoffPreservesKeyboardModes(t *testing.T) {
	liveKeyboardHandoff(t, "")
}

func liveKeyboardHandoff(t *testing.T, old string) {
	if old != "" {
		data, err := os.ReadFile(old)
		if err != nil {
			t.Fatal(err)
		}
		// Executable names select the real product namespace. Keep both
		// versions named ikesu so they share only this isolated test session.
		old = filepath.Join(t.TempDir(), "ikesu")
		if err = os.WriteFile(old, data, 0700); err != nil {
			t.Fatal(err)
		}
		t.Logf("unchanged old server SHA256=%x", sha256.Sum256(data))
	}
	s := startVersionMode(t, nil, false, binPath, old)
	s.waitFor("shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	dir := t.TempDir()
	control, capture, pids := filepath.Join(dir, "control"), filepath.Join(dir, "input"), filepath.Join(dir, "pids")
	s.send(fmt.Sprintf("python3 %q %q %q %q\r", filepath.Join(repoRoot(), "tests/parity/handoff_keyboard.py"), control, capture, pids))
	s.waitFor("real pane requested keyboard", 5*time.Second, func(text string) bool { return strings.Contains(text, "HANDOFF_KEY_start") })
	var want []byte
	check := func(raw, expected string) {
		t.Helper()
		want = append(want, expected...)
		s.send(raw)
		deadline := time.Now().Add(5 * time.Second)
		for {
			got, _ := os.ReadFile(capture)
			if len(got) >= len(want) {
				if !bytes.Equal(got, want) {
					t.Fatalf("pane bytes=%q want=%q", got, want)
				}
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("pane input timeout: got=%q want=%q", got, want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	first := "\x1b[97;1:1;97u"
	raw := "a"
	if old != "" {
		raw, first = "\x1b[97;5u", "\x1b[97;5u"
	}
	check(raw, first)
	hostFlags := func(want int) {
		t.Helper()
		s.text()
		flags, _ := s.emu.KeyboardModes()
		if flags != want {
			t.Fatalf("outer host flags=%d want=%d", flags, want)
		}
	}
	if old == "" {
		hostFlags(31)
	} else {
		hostFlags(7)
	}
	if out, err := s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
		t.Fatalf("handoff: %s %v", out, err)
	}
	check(raw, first)
	if old == "" {
		hostFlags(31)
	} else {
		hostFlags(7)
	}
	change := func(state string) {
		t.Helper()
		if err := os.WriteFile(control+".next", []byte(state), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(control+".next", control); err != nil {
			t.Fatal(err)
		}
		s.waitFor(state, 5*time.Second, func(text string) bool { return strings.Contains(text, "HANDOFF_KEY_"+state) })
	}
	change("pop1")
	if old == "" {
		check("\x1b[97;5u", "\x1b[97;5:1u")
	} else {
		check(raw, first)
	}
	if old == "" {
		hostFlags(31)
	}
	change("pop2")
	if old == "" {
		check("\x1b[97;5u", "\x01")
	} else {
		check("\x1b[97;5u", "\x01")
	}
	hostFlags(7)
	change("off")
	check("\x1b[97;5u", "\x01")
	identifiers, err := os.ReadFile(pids)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(identifiers))
	if len(lines) != 4 {
		t.Fatalf("PID observations=%q", identifiers)
	}
	for _, line := range lines {
		if line != lines[0] {
			t.Fatalf("pane PID changed: %q", identifiers)
		}
	}
	t.Logf("server replaced; pane PID %s stayed alive; exact flags, stack pops and modifyOtherKeys input preserved (old=%t)", lines[0], old != "")
}
