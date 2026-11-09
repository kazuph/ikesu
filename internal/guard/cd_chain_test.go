package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCdChainResolvesScriptInCdTarget(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "work", "dir")
	if e := os.MkdirAll(sub, 0700); e != nil {
		t.Fatal(e)
	}
	write := func(name, body string) {
		if e := os.WriteFile(filepath.Join(sub, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("s.sh", "echo hi\n")
	write("bad.sh", "rm -rf /outside/cd-chain-victim\n")
	write("file", "x")
	if e := os.MkdirAll(filepath.Join(root, "work", "other"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "work", "other", "s.sh"), []byte("rm -rf /outside/cd-chain-victim\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(sub, "sibling"), 0700); e != nil {
		t.Fatal(e)
	}
	i := Input{Cwd: root, LaunchCwd: root, Env: map[string]string{"PATH": "/usr/bin:/bin", "HOME": root}}
	rel := "work/dir"
	cases := []struct{ name, line, want string }{
		{"rel", "cd " + rel + " && bash s.sh", "allow"},
		{"abs", "cd " + sub + " && bash s.sh", "allow"},
		{"dot", "cd ./" + rel + " && bash s.sh", "allow"},
		{"real shape", "cd " + rel + " && python3 -c 'print(1)' && AI_GUARD_REQUEST_REASON='x' bash s.sh 2>&1 | grep -E \"ok|Error\" && echo ok", "allow"},
		{"danger script", "cd " + rel + " && bash bad.sh", "ask"},
		{"variable", "cd \"$X\" && bash s.sh", "ask"},
		{"subst", "cd $(pwd) && bash s.sh", "ask"},
		{"tilde", "cd ~/x && bash s.sh", "ask"},
		{"semicolon", "cd " + rel + "; bash s.sh", "ask"},
		{"or", "cd " + rel + " || bash s.sh", "ask"},
		{"two cd", "cd " + rel + " && cd . && bash s.sh", "ask"},
		{"option", "cd -P " + rel + " && bash s.sh", "ask"},
		{"prefix assign", "X=1 cd " + rel + " && bash s.sh", "ask"},
		{"redirect", "cd " + rel + " > /dev/null && bash s.sh", "ask"},
		{"missing dir", "cd work/nope && bash s.sh", "ask"},
		{"cd not first", "true && cd " + rel + " && bash s.sh", "ask"},
		{"eval cd", "cd " + rel + " && eval \"cd .\" && bash s.sh", "ask"},
		{"dotdot", "cd " + rel + "/../dir && bash s.sh", "ask"},
		{"zsh chdir", "cd " + rel + " && chdir ../other && bash s.sh", "ask"},
		{"chdir alone", "chdir " + rel + " && bash s.sh", "ask"},
		{"auto_cd parent", "cd " + rel + " && ../other && bash s.sh", "ask"},
		{"auto_cd dotdot", "cd " + rel + " && .. && bash s.sh", "ask"},
		{"auto_cd name", "cd " + rel + " && sibling && bash s.sh", "ask"},
		{"auto_cd minus", "cd " + rel + " && - && bash s.sh", "ask"},
		{"auto_cd tilde", "cd " + rel + " && ~- && bash s.sh", "ask"},
		{"dir stack plus", "cd +1 && bash s.sh", "ask"},
		{"non literal command", "cd " + rel + " && \"$X\" && bash s.sh", "ask"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, v, _ := lineVerdict(Config{}, i, tc.line)
			if v.Decision != tc.want {
				t.Fatalf("%s: got %+v want %s", tc.line, v, tc.want)
			}
		})
	}
	t.Run("rm is judged in the cd target", func(t *testing.T) {
		_, direct, _ := lineVerdict(Config{}, i, "rm "+filepath.Join(sub, "file"))
		_, viaCd, _ := lineVerdict(Config{}, i, "cd "+rel+" && rm ./file")
		if direct.Decision != viaCd.Decision || direct.Rule != viaCd.Rule {
			t.Fatalf("direct %+v via cd %+v", direct, viaCd)
		}
		_, outside, _ := lineVerdict(Config{}, i, "cd "+rel+" && rm ../../../outside-file-cd-chain")
		if outside.Decision != "ask" {
			t.Fatalf("outside %+v", outside)
		}
	})
	t.Run("cdpath", func(t *testing.T) {
		c := i
		c.Env = map[string]string{"PATH": "/usr/bin:/bin", "HOME": root, "CDPATH": root}
		_, v, _ := lineVerdict(Config{}, c, "cd "+rel+" && bash s.sh")
		if v.Decision != "ask" {
			t.Fatalf("%+v", v)
		}
		_, v, _ = lineVerdict(Config{}, c, "cd ./"+rel+" && bash s.sh")
		if v.Decision != "allow" {
			t.Fatalf("explicit ./ ignores CDPATH: %+v", v)
		}
	})
}
