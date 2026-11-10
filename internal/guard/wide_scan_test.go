package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWideScanVerdicts(t *testing.T) {
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	launch := filepath.Join(home, "src", "proj", "deep")
	sibling := filepath.Join(root, "x")
	for _, d := range []string{launch, sibling, filepath.Join(launch, "src")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "link-to-home")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HOME": home, "PATH": "/usr/bin:/bin"}
	_, usersErr := os.Stat("/Users")
	cases := []struct {
		cmd    string
		args   []string
		cwd    string
		reason string
		want   string
		needs  string
	}{
		{"find", []string{"/", "-name", "x"}, launch, "", "deny", ""},
		{"find", []string{home, "-name", "*.env"}, launch, "", "deny", ""},
		{"find", []string{"~", "-name", "*.env"}, launch, "", "deny", ""},
		{"find", []string{"-L", link, "-name", "x"}, launch, "", "deny", ""},
		{"find", []string{"-name", "x"}, filepath.Dir(launch), "", "deny", ""},
		{"rg", []string{"secret", "/Users"}, launch, "", "deny", "/Users"},
		{"grep", []string{"-r", "token", ".."}, launch, "", "deny", ""},
		{"grep", []string{"-rn", "token", filepath.Dir(launch)}, launch, "", "deny", ""},
		{"du", []string{"-sh", "~"}, launch, "", "deny", ""},
		{"ls", []string{"-R", "/"}, launch, "", "deny", ""},
		{"ls", []string{"-lR", "/"}, launch, "", "deny", ""},
		{"mdfind", []string{"password"}, launch, "", "deny", ""},
		{"mdfind", []string{"-onlyin", "/", "password"}, launch, "", "deny", ""},
		{"tree", []string{"~"}, launch, "", "deny", ""},
		{"locate", []string{"x"}, launch, "", "deny", ""},
		{"find", []string{"/", "-name", "x"}, launch, "ホームを調べる必要があるため", "ask", ""},
		{"find", []string{".", "-name", "x"}, launch, "", "allow", ""},
		{"find", []string{"-name", "x"}, launch, "", "allow", ""},
		{"rg", []string{"foo", "src"}, launch, "", "allow", ""},
		{"grep", []string{"-r", "x", "."}, launch, "", "allow", ""},
		{"grep", []string{"x", "/"}, launch, "", "allow", ""},
		{"ls", []string{"/"}, launch, "", "allow", ""},
		{"ls", []string{"-la", "/"}, launch, "", "allow", ""},
		{"du", []string{"-sh", "."}, launch, "", "allow", ""},
		{"find", []string{sibling}, launch, "", "allow", ""},
		{"mdfind", []string{"-onlyin", ".", "x"}, launch, "", "allow", ""},
		{"mdfind", []string{"-onlyin", sibling, "x"}, launch, "", "allow", ""},
	}
	for _, tc := range cases {
		if tc.needs == "/Users" && usersErr != nil {
			continue
		}
		e := map[string]string{}
		for k, v := range env {
			e[k] = v
		}
		if tc.reason != "" {
			e["AI_GUARD_REQUEST_REASON"] = tc.reason
		}
		v := Evaluate(Config{}, Input{Command: tc.cmd, Args: tc.args, Cwd: tc.cwd, LaunchCwd: launch, Env: e, wideScanLine: true})
		if v.Decision != tc.want {
			t.Errorf("%s %v: got %s (%s), want %s", tc.cmd, tc.args, v.Decision, v.Rule, tc.want)
		}
		if tc.want != "allow" && v.Rule != "wide-scan" {
			t.Errorf("%s %v: rule %s", tc.cmd, tc.args, v.Rule)
		}
		if tc.want == "deny" && (!strings.Contains(v.Reason, "【警告】大規模なスキャンを検知") || !strings.Contains(v.Reason, "AI_GUARD_REQUEST_REASON")) {
			t.Errorf("deny message: %s", v.Reason)
		}
	}
}

func TestWideScanLineAndLog(t *testing.T) {
	t.Parallel()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	launch := filepath.Join(root, "home", "proj")
	if err := os.MkdirAll(launch, 0o755); err != nil {
		t.Fatal(err)
	}
	i := Input{Cwd: launch, LaunchCwd: launch, Env: map[string]string{"HOME": filepath.Join(root, "home"), "PATH": "/usr/bin:/bin"}}
	_, v, err := lineVerdict(Config{}, i, "echo hi && find / -name x")
	if err != nil || v.Decision != "deny" || v.Rule != "wide-scan" {
		t.Fatalf("line: %v %+v", err, v)
	}
	_, v, err = lineVerdict(Config{}, i, "AI_GUARD_REQUEST_REASON='全体を探す必要があるため' find / -name x")
	if err != nil || v.Decision != "ask" || v.Rule != "wide-scan" {
		t.Fatalf("line with reason: %v %+v", err, v)
	}
	_, v, err = lineVerdict(Config{}, i, "find . -name x | rg foo")
	if err != nil || v.Decision != "allow" {
		t.Fatalf("line allow: %v %+v", err, v)
	}
}

func TestWideScanOnlyOnTopLevelLine(t *testing.T) {
	t.Parallel()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	launch := filepath.Join(root, "home", "proj")
	if err := os.MkdirAll(launch, 0o755); err != nil {
		t.Fatal(err)
	}
	base := Input{Command: "find", Args: []string{"/", "-name", "x"}, Cwd: launch, LaunchCwd: launch, Env: map[string]string{"HOME": filepath.Join(root, "home"), "PATH": "/usr/bin:/bin"}}
	// シム経由（guard exec）の入力は最上位の行ではないので発動しない。
	if v := Evaluate(Config{}, base); v.Decision != "allow" {
		t.Fatalf("shim path: %+v", v)
	}
	// DEBUG トラップの runtime 行は発動しない。
	runtime := base
	runtime.runtimeLine = true
	_, v, _ := lineVerdict(Config{}, runtime, "find / -name x")
	if v.Decision != "allow" {
		t.Fatalf("runtime line: %+v", v)
	}
	// スクリプトの中身は発動しない。
	script := base
	script.wideScanLine, script.scriptBody = true, true
	if v := Evaluate(Config{}, script); v.Decision != "allow" {
		t.Fatalf("script body: %+v", v)
	}
	// 既定の見張り対象に探索コマンドを足していない。
	for _, name := range []string{"find", "grep", "ls", "rg", "du", "mdfind"} {
		if has((Config{}).Commands(), name) {
			t.Fatalf("%s must not be shimmed", name)
		}
	}
}
