package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
				s := startMode(t, map[string]string{"IKESU_CONFIG": path}, local)
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
	s := startEnv(t, map[string]string{"IKESU_CONFIG": path})
	s.waitFor("shell prompt", 10*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.send("printf '%s_%s\\n' EXTERNAL CONFIG_OK\r")
	s.waitFor("shell output with external config", 10*time.Second, func(text string) bool { return strings.Contains(text, "EXTERNAL_CONFIG_OK") })
	if text := s.text(); strings.Contains(text, "continue") || strings.Contains(text, "RESIZE") || strings.Contains(text, "NAVIGATE") {
		t.Fatal("external onboarding=false ignored:", text)
	}
}

func TestOnboardingImmediateInputWhileConfigurationSaveWaits(t *testing.T) {
	path := onboardingConfig(t, "true")
	s := startEnv(t, map[string]string{"IKESU_CONFIG": path})
	s.waitFor("welcome", 10*time.Second, func(text string) bool { return strings.Contains(text, "continue") })
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	written := make(chan error, 1)
	go func() { <-release; written <- os.WriteFile(path, original, 0600) }()
	released := false
	unblock := func() {
		if !released {
			released = true
			close(release)
			if err := <-written; err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(unblock)
	s.send("\rprintf '%s_%s\\n' SAVE_WAIT INPUT_OK\r")
	s.waitFor("shell output while configuration save waits", 10*time.Second, func(text string) bool { return strings.Contains(text, "SAVE_WAIT_INPUT_OK") })
	unblock()
	s.waitFor("configuration persisted after input", 10*time.Second, func(string) bool {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
		data, err := os.ReadFile(path)
		return err == nil && strings.Contains(string(data), "onboarding = false")
	})
}
