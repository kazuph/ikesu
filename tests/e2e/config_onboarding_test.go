package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func onboardingConfig(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("onboarding="+value+"\n[session]\nresume_agents_on_restore=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOnboardingContinueImmediateShellInput(t *testing.T) {
	for _, local := range []bool{true, false} {
		mode := "session"
		if local {
			mode = "no-session"
		}
		for name, key := range map[string]string{"enter": "\r", "l": "l", "right": "\x1b[C"} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				path := onboardingConfig(t, "true")
				s := startMode(t, map[string]string{"GTX_CONFIG": path}, local)
				s.waitFor("welcome", 10*time.Second, func(text string) bool { return strings.Contains(text, "continue") })
				s.send(key + "printf '%s_%s\\n' IMMEDIATE INPUT_OK\r")
				s.waitFor("shell output after continue", 10*time.Second, func(text string) bool { return strings.Contains(text, "IMMEDIATE_INPUT_OK") })
				if text := s.text(); strings.Contains(text, "NAVIGATE") || strings.Contains(text, "RESIZE") || strings.Contains(text, "continue") {
					t.Fatal("unexpected mode after continue:", text)
				}
				data, err := os.ReadFile(path)
				if err != nil || !strings.Contains(string(data), "onboarding = false") {
					t.Fatal(string(data), err)
				}
			})
		}
	}
}

func TestServerInheritsExternalOnboardingConfig(t *testing.T) {
	path := onboardingConfig(t, "false")
	s := startEnv(t, map[string]string{"GTX_CONFIG": path})
	s.waitFor("shell prompt", 10*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.send("printf '%s_%s\\n' EXTERNAL CONFIG_OK\r")
	s.waitFor("shell output with external config", 10*time.Second, func(text string) bool { return strings.Contains(text, "EXTERNAL_CONFIG_OK") })
	if text := s.text(); strings.Contains(text, "continue") || strings.Contains(text, "RESIZE") || strings.Contains(text, "NAVIGATE") {
		t.Fatal("external onboarding=false ignored:", text)
	}
}
