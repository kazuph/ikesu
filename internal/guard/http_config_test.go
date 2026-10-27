package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalHTTPDefaultConfig(t *testing.T) {
	for _, tc := range []struct{ name, command, key, relative, contents string }{
		{"curl-home-connect", "curl", "HOME", ".curlrc", `connect-to = "localhost:80:example.invalid:80"`},
		{"curl-curlhome-location", "curl", "CURL_HOME", ".curlrc", "location\n"},
		{"curl-xdg-dot", "curl", "XDG_CONFIG_HOME", ".curlrc", `location`},
		{"curl-home-xdg-default", "curl", "HOME", ".config/curlrc", `location`},
		{"curl-curlhome-xdg-default", "curl", "CURL_HOME", ".config/curlrc", `location`},
		{"curl-xdg-url", "curl", "XDG_CONFIG_HOME", "curlrc", `url = "http://example.invalid"`},
		{"curl-proxy", "curl", "HOME", ".curlrc", `proxy = "http://example.invalid:8888"`},
		{"httpie-default-follow", "http", "HOME", ".config/httpie/config.json", `{"default_options":["--follow"]}`},
		{"httpie-xdg-proxy", "https", "XDG_CONFIG_HOME", "httpie/config.json", `{"default_options":["--proxy=http:http://example.invalid"]}`},
		{"httpie-custom", "http", "HTTPIE_CONFIG_DIR", "config.json", `{"default_options":["--follow"]}`},
		{"httpie-legacy", "http", "HOME", ".httpie/config.json", `{"default_options":["--follow"]}`},
		{"httpie-malformed", "http", "HOME", ".config/httpie/config.json", `{not-json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			env := map[string]string{"HOME": home}
			base := home
			if tc.key != "HOME" {
				base = t.TempDir()
				env[tc.key] = base
			}
			p := filepath.Join(base, tc.relative)
			if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(tc.contents), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"-XPOST", "http://localhost"}
			if tc.command != "curl" {
				args = []string{"POST", "http://localhost"}
			}
			i := Input{Command: tc.command, Args: args, Env: env, Cwd: home}
			if v := Evaluate(Config{}, i); v.Decision != "ask" {
				t.Fatalf("default config bypassed: %+v", v)
			}
			if tc.command == "curl" {
				for _, first := range []string{"-q", "--disable", "-qs"} {
					i.Args = append([]string{first}, args...)
					if v := Evaluate(Config{}, i); v.Decision != "allow" {
						t.Fatalf("leading %s: %+v", first, v)
					}
				}
				for _, a := range [][]string{append(append([]string{}, args...), "-q"), append([]string{"-sq"}, args...)} {
					i.Args = a
					if v := Evaluate(Config{}, i); v.Decision != "ask" {
						t.Fatalf("non-leading disable bypassed: %+v", v)
					}
				}
			}
		})
	}
}

func TestLocalHTTPUnknownConfig(t *testing.T) {
	for _, cmd := range []string{"curl", "http", "https"} {
		for _, kind := range []string{"unreadable", "directory", "dangling", "relative"} {
			t.Run(cmd+"/"+kind, func(t *testing.T) {
				home := t.TempDir()
				env := map[string]string{"HOME": home}
				p := filepath.Join(home, ".curlrc")
				args := []string{"-XPOST", "http://localhost"}
				if cmd != "curl" {
					p = filepath.Join(home, ".config/httpie/config.json")
					args = []string{"POST", "http://localhost"}
					if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				switch kind {
				case "unreadable":
					err = os.WriteFile(p, []byte("unknown"), 0000)
				case "directory":
					err = os.Mkdir(p, 0700)
				case "dangling":
					err = os.Symlink(filepath.Join(home, "missing"), p)
				case "relative":
					if cmd == "curl" {
						env["CURL_HOME"] = "relative-config"
					} else {
						env["HTTPIE_CONFIG_DIR"] = "relative-config"
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if v := Evaluate(Config{}, Input{Command: cmd, Args: args, Env: env, Cwd: home}); v.Decision != "ask" {
					t.Fatalf("unknown config allowed: %+v", v)
				}
			})
		}
	}
}
