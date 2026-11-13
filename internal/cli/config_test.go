package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/teststate"
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
		{"kitty", "[experimental]\nkitty_graphics=true\n", "config: ok\n", 0},
		{"unsupported", "[update]\nversion_check=true\n", "update.version_check: unsupported", 1},
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

func TestConfigNotifyJSON(t *testing.T) {
	teststate.Lock(t)
	p := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, config.EnvVar, p)
	run := func(content string) (string, int) {
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		code := Run([]string{"config", "notify", "--json"}, Options{Stdout: &out, Stderr: &stderr})
		return out.String(), code
	}
	if out, code := run(""); code != 0 || out != `{"enabled":false,"reach_timeout_ms":30000,"channels":[],"warnings":[]}`+"\n" {
		t.Fatal(code, out)
	}
	good := "[notify]\nreach_timeout=\"30s\"\n[[notify.channel]]\nid=\"android\"\nsend=\"now\"\n[[notify.channel]]\nid=\"slack\"\nsend=\"unanswered\"\nafter=\"5m\"\nfallback_for=[\"android\"]\n"
	want := `{"enabled":true,"reach_timeout_ms":30000,"channels":[{"id":"android","send":"now","after_ms":0,"fallback_for":[]},{"id":"slack","send":"unanswered","after_ms":300000,"fallback_for":["android"]}],"warnings":[]}` + "\n"
	if out, code := run(good); code != 0 || out != want {
		t.Fatal(code, out)
	}
	if out, code := run("[ui\n"); code == 0 || out != "" {
		t.Fatal(code, out)
	}
	// check も notify の警告を出す。
	if err := os.WriteFile(p, []byte("[notify]\n[[notify.channel]]\nid=\"x\"\nsend=\"now\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run([]string{"config", "check"}, Options{Stdout: &out, Stderr: &stderr}); code != 1 || !strings.Contains(out.String(), "notify.channel[0].id") {
		t.Fatal(code, out.String())
	}
}
