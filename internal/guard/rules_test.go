package guard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRules(t *testing.T) {
	dir := t.TempDir()
	i := Input{Cwd: dir, LaunchCwd: dir, Env: map[string]string{"HOME": dir, "PATH": "/usr/bin:/bin"}}
	cases := []struct {
		name, cmd string
		args      []string
		want      string
	}{
		{"protected", "echo", []string{".allow-main"}, "block"},
		{"amend", "git", []string{"commit", "--amend"}, "block"},
		{"force", "git", []string{"push", "--force-with-lease=refs/heads/a"}, "block"},
		{"reset", "git", []string{"reset", "--hard"}, "block"},
		{"wt-list", "git", []string{"wt", "list"}, "block"},
		{"wildcard-options", "gtx", []string{"msg", "send", "--room", "room", "*", "hi"}, "block"},
		{"named", "gtx", []string{"send", "p1", "deploy"}, "allow"},
		{"temp-delete", "rm", []string{"-rf", "/tmp/guard-proof"}, "allow"},
		{"artifacts-delete", "rm", []string{".artifacts/file"}, "allow"},
		{"traversal", "rm", []string{"/tmp/../file"}, "ask"},
		{"outside-delete", "rm", []string{"/Users/guard-proof"}, "ask"},
		{"deploy", "npx", []string{"wrangler", "deploy", "--env", "staging"}, "ask"},
		{"word-boundary", "echo", []string{"deployment"}, "allow"},
		{"gh-read", "gh", []string{"pr", "view", "1", "--json", "title"}, "allow"},
		{"gh-reply", "gh", []string{"api", "-X", "POST", "repos/a/b/pulls/comments/1/replies", "-f", "body=deploy"}, "allow"},
		{"gh-edit-comment", "gh", []string{"api", "repos/a/b/issues/comments/1", "--method=PATCH"}, "allow"},
		{"gh-write", "gh", []string{"api", "-XDELETE", "repos/a/b"}, "ask"},
		{"http-write", "curl", []string{"--data=body", "https://example.invalid"}, "ask"},
		{"aws-spec", "aws", []string{"s3", "ls"}, "ask"},
		{"wrangler-outside", "wrangler", []string{"whoami"}, "ask"},
		{"wrangler-pair", "wrangler", []string{"r2", "put", "x"}, "allow"},
		{"d1-read", "npx", []string{"wrangler@4", "d1", "execute", "db", "--command", "-- drop comment\n SELECT 1;"}, "allow"},
		{"d1-write", "wrangler", []string{"d1", "execute", "db", "--command=DELETE FROM t"}, "ask"},
		{"d1-file", "wrangler", []string{"d1", "execute", "db", "--file=x.sql", "--command=SELECT 1"}, "ask"},
		{"local", "wrangler", []string{"d1", "execute", "db", "--local", "--command=DELETE FROM t"}, "allow"},
		{"pragma", "mysql", []string{"-e", "PRAGMA table_info(t)"}, "allow"},
		{"multi-statement", "psql", []string{"-c", "SELECT 1; DELETE FROM t"}, "ask"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i.Command, i.Args = tc.cmd, tc.args
			v := Evaluate(Config{}, i)
			if v.Decision != tc.want || v.Rule == "" || v.Reason == "" {
				t.Fatalf("%+v want %s", v, tc.want)
			}
		})
	}
}
func TestApprovalKeysAndSuppression(t *testing.T) {
	d := physical(t.TempDir())
	f := filepath.Join(d, "approvals")
	i := Input{Command: "npx", Args: []string{"wrangler", "deploy", "--env", "staging"}, Cwd: d, Env: map[string]string{"HOME": d, "PATH": "/usr/bin:/bin", "AI_GUARD_TEMP_APPROVAL_FILE": f}}
	v := Evaluate(Config{}, i)
	if v.TempKey != "danger:npx:stg:"+d || v.DayKey != "day:"+d {
		t.Fatal(v)
	}
	if e := WriteApproval(f, v.TempKey, time.Now().Add(180*time.Second)); e != nil {
		t.Fatal(e)
	}
	if v = Evaluate(Config{}, i); v.Decision != "allow" || v.Suppressed != "temporary" {
		t.Fatal(v)
	}
	i.Command = "dd"
	i.Args = nil
	if v = Evaluate(Config{}, i); v.TempKey != "" || v.Decision != "ask" {
		t.Fatal(v)
	}
	if e := WriteApproval(f, "day:"+d, time.Now().Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	i.Command = "git"
	i.Args = []string{"reset"}
	if v = Evaluate(Config{}, i); v.Decision != "block" {
		t.Fatal(v)
	}
	st, _ := os.Stat(f)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
}
func TestConfigOverridesAndACK(t *testing.T) {
	i := Input{Command: "echo", Args: []string{"release"}, Cwd: t.TempDir(), Env: map[string]string{}}
	if v := Evaluate(Config{DangerWords: []string{"release"}}, i); v.Decision != "ask" {
		t.Fatal(v)
	}
	i.Env["AI_GUARD_DANGER_WORD_ACK"] = "1"
	if v := Evaluate(Config{DangerWords: []string{"release"}}, i); v.Decision != "allow" {
		t.Fatal(v)
	}
	if v := Evaluate(Config{ProtectedPatterns: []string{"release"}}, i); v.Decision != "block" {
		t.Fatal(v)
	}
}
