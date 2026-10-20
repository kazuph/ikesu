//go:build linux

package notify

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
)

func TestLinuxSoundSelectionAndMissingPlayerKeepsNotification(t *testing.T) {
	t.Setenv("GTX_DISABLE_SOUND", "")
	candidates := []string{"paplay", "pw-play", "ffplay", "mpg123", "mpv"}
	for selected := 0; selected <= len(candidates); selected++ {
		name := "missing"
		if selected < len(candidates) {
			name = candidates[selected]
		}
		t.Run(name, func(t *testing.T) {
			var probes []string
			var calls [][]string
			var warnings []string
			d := Desktop{ConfigPath: "/tmp/config.toml", soundWarning: &sync.Once{},
				LookPath: func(name string) (string, error) {
					if name == "notify-send" {
						return "/test/notify-send", nil
					}
					probes = append(probes, name)
					for _, available := range candidates[selected:] {
						if name == available {
							return "/test/" + name, nil
						}
					}
					return "", errors.New("not installed")
				},
				Run: func(name string, args ...string) error {
					calls = append(calls, append([]string{name}, args...))
					return nil
				},
				Warn: func(err error) { warnings = append(warnings, err.Error()) },
			}
			c := config.Config{}
			c.UI.Sound.Path = "custom.mp3"
			n := endpoint.Notification{SystemToast: true, Title: "title", Body: "body", Agent: "codex", Kind: "finished"}
			for range 2 {
				if err := d.Deliver(n, c, "test", "local"); err != nil {
					t.Fatal(err)
				}
			}
			if selected == len(candidates) {
				if len(calls) != 2 || len(warnings) != 1 || !strings.Contains(warnings[0], "no command found") {
					t.Fatal(calls, warnings)
				}
				for _, candidate := range candidates {
					if !strings.Contains(warnings[0], candidate) {
						t.Fatal(warnings)
					}
				}
			} else {
				if len(calls) != 4 || len(warnings) != 0 {
					t.Fatal(calls, warnings)
				}
				args := []string{"/test/" + name, "/tmp/custom.mp3"}
				switch name {
				case "ffplay":
					args = []string{"/test/ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", "/tmp/custom.mp3"}
				case "mpg123":
					args = []string{"/test/mpg123", "-q", "/tmp/custom.mp3"}
				case "mpv":
					args = []string{"/test/mpv", "--no-video", "--really-quiet", "/tmp/custom.mp3"}
				}
				if !reflect.DeepEqual(calls[1], args) || !reflect.DeepEqual(calls[3], args) {
					t.Fatal(calls, args)
				}
			}
			count := selected + 1
			if selected == len(candidates) {
				count = selected
			}
			expected := append(append([]string{}, candidates[:count]...), candidates[:count]...)
			if !reflect.DeepEqual(probes, expected) {
				t.Fatal(probes, expected)
			}
			for _, call := range calls {
				if call[0] == "/test/notify-send" && !reflect.DeepEqual(call, []string{"/test/notify-send", "--", "title", "body"}) {
					t.Fatal(call)
				}
			}
		})
	}
}
