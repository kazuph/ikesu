package guard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type globFixture struct {
	root, repo, other, wt, wt2 string
	in                         Input
}

func newGlobFixture(t *testing.T) globFixture {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(home, ".ikesu-guard-glob-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	root = guardPhysical(root)
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = root
		c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	f := globFixture{root: root, repo: filepath.Join(root, "R"), other: filepath.Join(root, "other"), wt: filepath.Join(root, "R", ".worktree", "wt1"), wt2: filepath.Join(root, "R", ".worktree", "wt2")}
	run("init", f.repo)
	run("-C", f.repo, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "init")
	run("-C", f.repo, "worktree", "add", "-b", "b1", f.wt)
	run("-C", f.repo, "worktree", "add", "-b", "b2", f.wt2)
	run("init", f.other)
	for _, d := range []string{filepath.Join(f.wt, "out"), filepath.Join(f.other, "sub")} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{filepath.Join(f.wt, "out", "a.jpg"), filepath.Join(f.wt, "out", "b.jpg"), filepath.Join(f.other, "sub", "f"), filepath.Join(f.wt, "plain.txt"), filepath.Join(f.wt, ".gx")} {
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(f.other, filepath.Join(f.wt, "lnk")); err != nil {
		t.Fatal(err)
	}
	f.in = Input{Cwd: f.wt, LaunchCwd: f.wt, Env: map[string]string{"PATH": "/usr/bin:/bin:/opt/homebrew/bin", "HOME": home}}
	return f
}

func (f globFixture) decide(args ...string) string {
	i := f.in
	i.Command = "rm"
	i.Args = args
	return Evaluate(Config{}, i).Decision
}

func TestRemovalGlobAndRegisteredWorktrees(t *testing.T) {
	f := newGlobFixture(t)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"ordinary file", []string{"out/a.jpg"}, "allow"},
		{"glob of ordinary files", []string{"out/*.jpg"}, "allow"},
		{"glob of ordinary files absolute", []string{filepath.Join(f.wt, "out") + "/*.jpg"}, "allow"},
		{"glob with no match keeps literal", []string{"nomatch_*.jpg"}, "allow"},
		{"quoted prefix glob", []string{"-f", "who_f*.jpg"}, "allow"},
		{"dot glob reaches .git", []string{"-r", ".g*"}, "ask"},
		{"class glob reaches .git", []string{"[.]git"}, "ask"},
		{"dot star", []string{"-r", ".*"}, "ask"},
		{"star reaches symlink to other repo", []string{"-r", "*/sub/f"}, "ask"},
		{"brace", []string{"out/{a,b}.jpg"}, "ask"},
		{"tilde", []string{"~/x"}, "ask"},
		{"parent of registered worktrees", []string{"-r", f.repo + "/.worktree"}, "ask"},
		{"registered worktree itself", []string{"-r", f.wt2}, "ask"},
		{"dir without worktrees", []string{"-r", "out"}, "allow"},
	}
	for _, tc := range cases {
		if got := f.decide(tc.args...); got != tc.want {
			t.Errorf("%s %q: got %s want %s", tc.name, tc.args, got, tc.want)
		}
	}
	// A directory under /tmp or .artifacts that holds a registered worktree.
	art := filepath.Join(f.wt, ".artifacts")
	if err := os.MkdirAll(art, 0700); err != nil {
		t.Fatal(err)
	}
	if got := f.decide("-r", art); got != "allow" {
		t.Errorf(".artifacts plain dir: %s", got)
	}
	if got := f.decide(art + "/{a,b}"); got != "ask" {
		t.Errorf(".artifacts brace: %s", got)
	}
}

func TestRemovalGlobTooManyMatches(t *testing.T) {
	f := newGlobFixture(t)
	d := filepath.Join(f.wt, "many")
	if err := os.Mkdir(d, 0700); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2100; n++ {
		if err := os.WriteFile(filepath.Join(d, fmt.Sprintf("f%d.tmp", n)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.decide("many/*.tmp"); got != "ask" {
		t.Errorf("too many matches: %s", got)
	}
}

func TestRemovalManyDirectoriesIsFast(t *testing.T) {
	f := newGlobFixture(t)
	var args []string
	for n := 0; n < 100; n++ {
		d := filepath.Join(f.wt, fmt.Sprintf("d%d", n))
		if err := os.MkdirAll(filepath.Join(d, "out"), 0700); err != nil {
			t.Fatal(err)
		}
		args = append(args, filepath.Join(d, "out", "x"))
	}
	start := time.Now()
	got := f.decide(args...)
	elapsed := time.Since(start)
	t.Logf("100 directories: %s in %s", got, elapsed)
	if got != "allow" {
		t.Errorf("got %s", got)
	}
	if elapsed > 10*time.Second {
		t.Errorf("too slow: %s", elapsed)
	}
}

func TestCurlBodyOptionsAndAbbreviations(t *testing.T) {
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin", "HOME": t.TempDir()}}
	for _, tc := range []struct{ line, want string }{
		{`curl --data-ascii a https://example.com`, "ask"},
		{`curl -T f https://example.com`, "ask"},
		{`curl --upload-file f https://example.com`, "ask"},
		{`curl --form-string a=b https://example.com`, "ask"},
		{`curl --reque POST https://example.com`, "ask"},
		{`curl --dat a https://example.com`, "ask"},
		{`curl --upload f https://example.com`, "ask"},
		{`curl https://example.com`, "allow"},
		{`curl --head https://example.com`, "allow"},
		{`curl -w %{http_code} http://localhost:1`, "allow"},
	} {
		_, v, _ := lineVerdict(Config{}, i, tc.line)
		if v.Decision != tc.want {
			t.Errorf("%s: got %+v want %s", tc.line, v, tc.want)
		}
	}
}
