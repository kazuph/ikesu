package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScriptFileHomebrewInstalledBash(t *testing.T) {
	i, _ := scriptPane(t)
	script := filepath.Join(i.Cwd, "program.sh")
	if e := os.WriteFile(script, []byte("read TARGET; rm \"$TARGET\"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	installations := 0
	for _, prefix := range []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"} {
		link := filepath.Join(prefix, "bin", "bash")
		real, e := filepath.EvalSymlinks(link)
		if e != nil {
			continue
		}
		installations++
		for _, command := range []string{link, real, "bash"} {
			next := i
			next.Env = EnvMap(environment(i.Env))
			next.Env["PATH"] = filepath.Join(prefix, "bin") + ":/usr/bin:/bin"
			_, v, _ := lineVerdict(Config{}, next, quote(command)+" "+quote(script))
			if v.Decision != "allow" {
				t.Errorf("%s (%s): got %+v want allow", command, real, v)
			}
		}
	}
	if installations == 0 {
		t.Skip("標準の場所にHomebrewのbashがないため、実物を使う正例だけをスキップします")
	}
}

func TestHomebrewBashPathContract(t *testing.T) {
	for _, prefix := range []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"} {
		path := filepath.Join(prefix, "Cellar", "bash", "5.3.9", "bin", "bash")
		if !homebrewBashPath(path) {
			t.Errorf("standard Cellar rejected: %s", path)
		}
	}
	for _, path := range []string{
		"/opt/homebrew/bin/bash",
		"/opt/homebrew/Cellar/zsh/5.9/bin/bash",
		"/opt/homebrew/Cellar/bash-completion/2.0/bin/bash",
		"/opt/homebrew/Cellar/bash/5.3.9/libexec/bin/bash",
		"/opt/homebrew/Cellar/bash/bin/bash",
		"/opt/homebrew/Cellar/bash/5.3.9/bin/dash",
		"/opt/homebrew-other/Cellar/bash/5.3.9/bin/bash",
		"/custom/homebrew/Cellar/bash/5.3.9/bin/bash",
		"/tmp/opt/homebrew/Cellar/bash/5.3.9/bin/bash",
		"Cellar/bash/5.3.9/bin/bash",
	} {
		if homebrewBashPath(path) {
			t.Errorf("untrusted location accepted: %s", path)
		}
	}
}

func TestScriptFileHomebrewUntrustedCopies(t *testing.T) {
	i, _ := scriptPane(t)
	script := filepath.Join(i.Cwd, "program.sh")
	if e := os.WriteFile(script, []byte("read TARGET; rm \"$TARGET\"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	realBash, e := os.ReadFile("/bin/bash")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"bin/bash", "opt/homebrew/Cellar/zsh/5.9/bin/bash", "custom/homebrew/Cellar/bash/5.3.9/bin/bash", "opt/homebrew/Cellar/bash/5.3.9/bin/bash"} {
		path := filepath.Join(i.Cwd, name)
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, realBash, 0700); e != nil {
			t.Fatal(e)
		}
		next := i
		next.Env = EnvMap(environment(i.Env))
		next.Env["PATH"] = filepath.Dir(path) + ":/usr/bin:/bin"
		for _, command := range []string{path, "bash"} {
			_, v, _ := lineVerdict(Config{}, next, quote(command)+" "+quote(script))
			if v.Decision != "ask" {
				t.Fatalf("nonstandard real bash copy %s: %+v", command, v)
			}
		}
	}
}
