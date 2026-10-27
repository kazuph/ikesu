package guard

import "testing"

func TestLocalHTTPDestinations(t *testing.T) {
	allowed := []string{"http://localhost/x", "http://LOCALHOST.:3847/webhook/stop", "http://127.0.0.1", "http://127.255.255.255", "http://[::1]:3847/x", "https://mini.tail123.ts.net/x", "https://MINI.TAIL123.TS.NET./x", "http://100.64.0.0", "http://100.127.255.255", "http://[fd7a:115c:a1e0::1]", "http://[fd7a:115c:a1e0:ffff:ffff:ffff:ffff:ffff]"}
	denied := []string{"http://10.0.0.1", "http://172.16.0.1", "http://192.168.0.1", "https://example.com", "http://126.255.255.255", "http://128.0.0.0", "http://100.63.255.255", "http://100.128.0.0", "http://[fd7a:115c:a1df::1]", "http://[fd7a:115c:a1e1::1]", "http://localhost.evil.com", "http://evilts.net", "http://ts.net", "http://localhost@evil.com", "http://user@localhost", "http://$HOST/x", "http://${HOST}/x", "http://$(hostname)/x", "http://localhost/{x,y}", "http://[::ffff:127.0.0.1]", "file://localhost/x"}
	for _, cmd := range []string{"curl", "http", "https"} {
		for _, group := range []struct {
			urls []string
			want string
		}{{allowed, "allow"}, {denied, "ask"}} {
			for _, u := range group.urls {
				t.Run(cmd+"/"+u, func(t *testing.T) {
					a := []string{"-X", "POST", u}
					if cmd != "curl" {
						a = []string{"POST", u}
					}
					i := Input{Command: cmd, Args: a, Cwd: t.TempDir(), Env: map[string]string{}}
					if v := Evaluate(Config{}, i); v.Decision != group.want {
						t.Fatalf("%+v want %s", v, group.want)
					}
				})
			}
		}
	}
}

func TestLocalHTTPConnectionOverrides(t *testing.T) {
	cases := []struct {
		name, cmd  string
		args       []string
		env        map[string]string
		unresolved bool
		want       string
	}{
		{"hook", "curl", []string{"-s", "-X", "POST", "http://localhost:3847/webhook/stop", "-H", "Content-Type: application/json", "-d", `{"ok":true}`}, nil, false, "allow"},
		{"combined", "curl", []string{"-sS", "-XPOST", "--url=http://localhost/x", "--data=body"}, nil, false, "allow"},
		{"all-local", "curl", []string{"-XPOST", "http://localhost", "http://100.64.0.1"}, nil, false, "allow"},
		{"mixed", "curl", []string{"-XPOST", "http://localhost", "http://example.com"}, nil, false, "ask"},
		{"url-option", "curl", []string{"-XPOST", "--url", "http://localhost"}, nil, false, "allow"},
		{"redirect", "curl", []string{"-XPOST", "-L", "http://localhost"}, nil, false, "ask"},
		{"redirect-combined", "curl", []string{"-sSL", "-XPOST", "http://localhost"}, nil, false, "ask"},
		{"location", "curl", []string{"-XPOST", "--location", "http://localhost"}, nil, false, "ask"},
		{"resolve", "curl", []string{"-XPOST", "--resolve", "localhost:80:1.2.3.4", "http://localhost"}, nil, false, "ask"},
		{"connect-to", "curl", []string{"-XPOST", "--connect-to=localhost:80:evil.com:80", "http://localhost"}, nil, false, "ask"},
		{"proxy", "curl", []string{"-XPOST", "--proxy=http://evil.com", "http://localhost"}, nil, false, "ask"},
		{"short-proxy", "curl", []string{"-XPOST", "-x", "http://evil.com", "http://localhost"}, nil, false, "ask"},
		{"config", "curl", []string{"-XPOST", "-K", "settings", "http://localhost"}, nil, false, "ask"},
		{"proxy-env", "curl", []string{"-XPOST", "http://localhost"}, map[string]string{"http_proxy": "http://evil.com"}, false, "ask"},
		{"proxy-env-upper", "http", []string{"POST", "http://localhost"}, map[string]string{"HTTPS_PROXY": "http://evil.com"}, false, "ask"},
		{"proxy-env-all", "curl", []string{"-XPOST", "http://localhost"}, map[string]string{"ALL_PROXY": "socks5://evil.com"}, false, "ask"},
		{"unresolved", "curl", []string{"-XPOST", "http://localhost"}, nil, true, "ask"},
		{"missing", "curl", []string{"-XPOST"}, nil, false, "ask"},
		{"wget", "wget", []string{"--method=POST", "http://localhost"}, nil, false, "ask"},
		{"httpie-follow", "http", []string{"--follow", "POST", "http://localhost"}, nil, false, "ask"},
		{"danger-preserved", "curl", []string{"-XPOST", "http://localhost/deploy"}, nil, false, "ask"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := Input{Command: tc.cmd, Args: tc.args, Env: tc.env, Unresolved: tc.unresolved, Cwd: t.TempDir()}
			if v := Evaluate(Config{}, i); v.Decision != tc.want {
				t.Fatalf("%+v want %s", v, tc.want)
			}
		})
	}
}
