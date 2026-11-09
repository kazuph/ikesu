package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRemovalSameRepositoryWorktree(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(home, ".ikesu-guard-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = root
		c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	repo := filepath.Join(root, "repo")
	other := filepath.Join(root, "other")
	wt := filepath.Join(root, "worktree")
	run("init", repo)
	run("-C", repo, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "init")
	run("-C", repo, "worktree", "add", "-b", "test", wt)
	run("init", other)
	if err := os.Mkdir(filepath.Join(wt, "out"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(wt, "other-dir"), 0700); err != nil {
		t.Fatal(err)
	}
	i := Input{Cwd: filepath.Join(wt, "out"), LaunchCwd: repo, Env: map[string]string{"PATH": "/usr/bin:/bin", "HOME": home}}
	for _, tc := range []struct {
		path string
		want bool
	}{{filepath.Join(wt, "out", "file"), true}, {wt, false}, {filepath.Join(wt, ".git"), false}, {repo, false}, {home, false}, {filepath.Join(other, "file"), false}, {"../file", false}} {
		if got := safeRemoval(i, tc.path); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.path, got, tc.want)
		}
	}
	link := filepath.Join(wt, "out", "link")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link + "/../file", link + "/file", filepath.Join(wt, "out") + "/../file"} {
		v := Evaluate(Config{}, Input{Command: "rm", Args: []string{path}, Cwd: i.Cwd, LaunchCwd: i.LaunchCwd, Env: i.Env})
		if v.Decision != "ask" {
			t.Errorf("symlink or parent %s: %+v", path, v)
		}
	}
	v := Evaluate(Config{}, Input{Command: "rm", Args: []string{filepath.Join(wt, "out", "file")}, Cwd: i.Cwd, LaunchCwd: i.LaunchCwd, Env: i.Env})
	if v.Decision != "allow" {
		t.Errorf("guard check removal: %+v", v)
	}
	// Launched in one worktree, removing inside a sibling worktree of the same repository.
	wt2 := filepath.Join(root, "worktree2")
	run("-C", repo, "worktree", "add", "-b", "test2", wt2)
	sibling := i
	sibling.LaunchCwd = wt2
	if !safeRemoval(sibling, filepath.Join(wt, "out", "file")) {
		t.Error("sibling worktree of the launch worktree should be removable")
	}
	for _, path := range []string{wt, filepath.Join(wt, ".git"), repo, filepath.Join(other, "file")} {
		if safeRemoval(sibling, path) {
			t.Errorf("sibling launch: %s should still ask", path)
		}
	}
	spoofed := i
	spoofed.Cwd = other
	spoofed.Env = map[string]string{"PATH": "/usr/bin:/bin", "HOME": home, "GIT_DIR": filepath.Join(repo, ".git"), "GIT_WORK_TREE": other}
	if safeRemoval(spoofed, filepath.Join(other, "file")) {
		t.Error("git environment made a different repository look like the launch repository")
	}

}

func TestTemporaryWorktreeRootStillRequiresApproval(t *testing.T) {
	d := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", d).CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	if err := os.Mkdir(filepath.Join(d, "launch"), 0700); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", other).CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	i := Input{Cwd: d, LaunchCwd: filepath.Join(d, "launch"), Env: map[string]string{"PATH": "/usr/bin:/bin", "HOME": os.Getenv("HOME")}}
	for _, tc := range []struct {
		path string
		want bool
	}{{d, false}, {filepath.Join(d, ".git"), false}, {filepath.Join(d, "output"), true}, {filepath.Join(other, "output"), false}} {
		if got := safeRemoval(i, tc.path); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.path, got, tc.want)
		}
	}
}

func TestCurlWriteOutAndValueOptions(t *testing.T) {
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin", "HOME": t.TempDir()}}
	for _, tc := range []struct{ line, want string }{
		{`curl -s -w '\nHTTP %{http_code}\n' -X POST -d x https://ubuntu-3090.tail5f04b.ts.net:18443/echo-family/api/say`, "allow"},
		{`curl --write-out '%{http_code}' -X POST -d x http://localhost`, "allow"},
		{`curl --write-out=%{http_code} -X POST -d x http://localhost`, "allow"},
		{`curl -A agent -e http://referrer.invalid -b session=1 -u user:pass -r 0-10 -Y 1 -y 2 --retry 1 --limit-rate 1k --url-query a=b -X POST http://localhost`, "allow"},
		{`curl -w '%output{/tmp/file}%{http_code}' -X POST http://localhost`, "ask"},
		{`curl -w @format-file -X POST http://localhost`, "ask"},
		{`curl -w '%{http_code}' -X POST -d x https://example.com`, "ask"},
		{`curl -w '-XGET' -d x https://example.com`, "ask"},
		{`curl --resolve localhost:80:example.com -w '%{http_code}' -X POST http://localhost`, "ask"},
		{`curl --proxy http://example.com -w '%{http_code}' -X POST http://localhost`, "ask"},
	} {
		_, v, _ := lineVerdict(Config{}, i, tc.line)
		if v.Decision != tc.want {
			t.Errorf("%s: got %+v want %s", tc.line, v, tc.want)
		}
	}
}

func TestGuardCheckCurlWriteOut(t *testing.T) {
	i := Input{Command: "curl", Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin", "HOME": t.TempDir()}}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-s", "-w", `\nHTTP %{http_code}\n`, "-X", "POST", "-d", "x", "https://ubuntu-3090.tail5f04b.ts.net:18443/echo-family/api/say"}, "allow"},
		{[]string{"-s", "-X", "POST", "-d", "x", "https://ubuntu-3090.tail5f04b.ts.net:18443/echo-family/api/say"}, "allow"},
		{[]string{"-w", "-XGET", "-d", "x", "https://example.com"}, "ask"},
	} {
		i.Args = tc.args
		if v := Evaluate(Config{}, i); v.Decision != tc.want {
			t.Errorf("%q: %+v", tc.args, v)
		}
	}
}

func TestRemovedShellExceptionsRemainAsk(t *testing.T) {
	d := t.TempDir()
	i := Input{Cwd: d, LaunchCwd: d, Env: map[string]string{"PATH": "/usr/bin:/bin", "HOME": d}}
	if err := os.Mkdir(filepath.Join(d, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "child", "ok.sh"), []byte("printf ok\n"), 0600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(d, "literal.sh")
	if err := os.WriteFile(script, []byte("D=/tmp\nprintf x > \"$D/out\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ line, want string }{
		{"bash " + quote(script), "allow"},  // 変数の書き込み先は未確定でも聞かない（2026-11-10）
		{"cd child && bash ok.sh", "allow"}, // 0.4.121: cd 先の安全なスクリプトを判定する
		{`eval "$(direnv export zsh)"`, "ask"},
	} {
		_, v, err := lineVerdict(Config{}, i, tc.line)
		if err != nil || v.Decision != tc.want {
			t.Errorf("%s: %+v %v", tc.line, v, err)
		}
	}
}
