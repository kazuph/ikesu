package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/config"
)

func TestEndpointSettingsWriteClientConfigurationAndKeepShellInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-config.toml")
	if err := os.WriteFile(path, []byte("onboarding=false\n[extra]\nkeep='endpoint-value'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := startEnv(t, map[string]string{"GTX_CONFIG": path})
	s.waitFor("shell", 10*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.send("\x02s")
	s.waitFor("settings", 5*time.Second, func(text string) bool { return strings.Contains(text, "settings") && strings.Contains(text, "toasts") })
	s.send("\x1b[B\r")
	s.waitFor("saved settings", 5*time.Second, func(text string) bool {
		data, err := os.ReadFile(path)
		return err == nil && strings.Contains(string(data), config.ThemeNames[1]) && !strings.Contains(text, " settings")
	})
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "endpoint-value") {
		t.Fatal("unrelated configuration lost", err)
	}
	s.send("printf '%s_%s\\n' SETTINGS INPUT_OK\r")
	s.waitFor("shell after settings apply", 5*time.Second, func(text string) bool { return strings.Contains(text, "SETTINGS_INPUT_OK") })
}
