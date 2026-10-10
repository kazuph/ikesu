package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestConfigCheckWithoutTerminalOrServer(t *testing.T) {
	teststate.Lock(t)
	p := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, config.EnvVar, p)
	for _, tc := range []struct {
		name, content, want string
		code                int
	}{
		{"missing", "", "config: ok\n", 0},
		{"valid", "onboarding=false\n", "config: ok\n", 0},
		{"unknown", "[ui]\nbogus=1\n", "config: ok\n", 0},
		{"type", "[ui]\nsidebar_width='bad'\n", "ui.sidebar_width", 1},
		{"binding", "[keys]\nnew_tab='prefix+zz'\n", "keys.new_tab", 1},
		{"unsupported", "[experimental]\nkitty_graphics=true\n", "experimental.kitty_graphics: unsupported", 1},
		{"syntax", "[ui\n", "config parse error:", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name != "missing" {
				if err := os.WriteFile(p, []byte(tc.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var out, stderr bytes.Buffer
			code := Run([]string{"config", "check"}, Options{Stdout: &out, Stderr: &stderr})
			if code != tc.code || !strings.Contains(out.String(), tc.want) || stderr.Len() != 0 {
				t.Fatal(code, out.String(), stderr.String())
			}
		})
	}
}
