package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// git の全体オプション（-C など）が前に付いても、git の決まりが効く。
func TestGitGlobalOptionsKeepRules(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{"HOME": home, "PATH": "/usr/bin:/bin:/opt/homebrew/bin"}
	git, e := Resolve("git", environment(env))
	if e != nil {
		t.Fatal(e)
	}
	run := func(dir string, args ...string) {
		c := exec.Command(git, args...)
		c.Dir, c.Env = dir, environment(env)
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatal(string(b), e)
		}
	}
	mainRepo, featRepo, permitRepo := t.TempDir(), t.TempDir(), t.TempDir()
	run(mainRepo, "init", "-q", "--initial-branch=main")
	run(featRepo, "init", "-q", "--initial-branch=feature")
	run(permitRepo, "init", "-q", "--initial-branch=main")
	os.WriteFile(filepath.Join(permitRepo, ".allow-main"), nil, 0600)
	elsewhere := t.TempDir()
	judge := func(cwd string, args ...string) Verdict {
		return Evaluate(Config{}, Input{Command: "git", Args: args, Cwd: cwd, LaunchCwd: cwd, Env: env})
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
		rule string
	}{
		{"-C main push", []string{"-C", mainRepo, "push", "origin", "main"}, "block", "git-main"},
		{"-C main push HEAD", []string{"-C", mainRepo, "push"}, "block", "git-main"},
		{"-C force", []string{"-C", featRepo, "push", "--force", "origin", "x"}, "block", "git-push-force"},
		{"-C reset", []string{"-C", featRepo, "reset", "--hard"}, "block", "git-reset"},
		{"-C amend", []string{"-C", featRepo, "commit", "--amend"}, "block", "git-commit-amend"},
		{"-c -C main", []string{"-c", "user.name=x", "-C", mainRepo, "push", "origin", "main"}, "block", "git-main"},
		{"no-pager -C main", []string{"--no-pager", "-C", mainRepo, "push"}, "block", "git-main"},
		{"git-dir main", []string{"--git-dir=" + filepath.Join(mainRepo, ".git"), "push", "origin", "main"}, "block", "git-main"},
		{"-C clean", []string{"-C", featRepo, "clean", "-fd"}, "ask", ""},
		{"-C stash drop", []string{"-C", featRepo, "stash", "drop"}, "ask", ""},
		{"-C permit", []string{"-C", permitRepo, "push"}, "allow", "main-permit"},
		{"-C status", []string{"-C", featRepo, "status"}, "allow", ""},
		{"-C log", []string{"-C", featRepo, "log"}, "allow", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := judge(elsewhere, tc.args...)
			if v.Decision != tc.want || tc.rule != "" && v.Rule != tc.rule {
				t.Fatalf("got %+v want %s %s", v, tc.want, tc.rule)
			}
		})
	}
	t.Run("-C feature push equals plain push in that directory", func(t *testing.T) {
		got := judge(elsewhere, "-C", featRepo, "push", "origin", "feature")
		want := judge(featRepo, "push", "origin", "feature")
		if got.Decision != want.Decision || got.Rule != want.Rule {
			t.Fatalf("got %+v want %+v", got, want)
		}
	})
	t.Run("relative -C joins the current directory", func(t *testing.T) {
		v := judge(filepath.Dir(mainRepo), "-C", filepath.Base(mainRepo), "push")
		if v.Rule != "git-main" {
			t.Fatalf("%+v", v)
		}
	})
}
