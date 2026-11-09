package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHomebrewDirenvPathContract(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"} {
		if path := filepath.Join(prefix, "Cellar", "direnv", "2.37.1", "bin", "direnv"); !homebrewDirenvPath(path) {
			t.Errorf("standard Cellar rejected: %s", path)
		}
	}
	for _, path := range []string{
		"/opt/homebrew/bin/direnv",
		"/opt/homebrew/Cellar/bash/5.3.9/bin/direnv",
		"/opt/homebrew/Cellar/direnv/bin/direnv",
		"/opt/homebrew/Cellar/direnv/2.37.1/libexec/bin/direnv",
		"/opt/homebrew/Cellar/direnv/2.37.1/bin/other",
		"/tmp/opt/homebrew/Cellar/direnv/2.37.1/bin/direnv",
		"/custom/Cellar/direnv/2.37.1/bin/direnv",
		"Cellar/direnv/2.37.1/bin/direnv",
	} {
		if homebrewDirenvPath(path) {
			t.Errorf("untrusted location accepted: %s", path)
		}
	}
}

func TestTrustedDirenvLineShapes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	real := filepath.Join(dir, "Cellar", "direnv", "2.37.1", "bin", "direnv")
	if e := os.MkdirAll(filepath.Dir(real), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(real, []byte("x"), 0700); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(dir, "bin", "direnv")
	if e := os.MkdirAll(filepath.Dir(link), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(real, link); e != nil {
		t.Fatal(e)
	}
	other := filepath.Join(dir, "other-direnv")
	if e := os.WriteFile(other, []byte("x"), 0700); e != nil {
		t.Fatal(e)
	}
	resolved, _ := filepath.EvalSymlinks(real)
	envrc := "/work/proj/.envrc"
	good := `eval "$("` + link + `" stdlib)" && __main__ source_env $'` + envrc + `'`
	mainFrames := map[string]map[int]string{"FUNCNAME": {0: "_ikesu_guard_debug", 1: "_ikesu_guard_dispatch", 2: "__main__"}}
	entryFrames := map[string]map[int]string{"FUNCNAME": {0: "_ikesu_guard_debug", 1: "_ikesu_guard_dispatch", 2: "source_env", 3: "__main__"}}
	for _, tc := range []struct {
		name   string
		line   string
		parent string
		arrays map[string]map[int]string
		want   bool
	}{
		{"exact", good, resolved, nil, true},
		{"plain literal path", `eval "$("` + link + `" stdlib)" && __main__ source_env ` + envrc, resolved, nil, true},
		{"eval half", `eval "$("` + link + `" stdlib)"`, resolved, nil, true},
		{"main frame", `"$@"`, resolved, mainFrames, true},
		{"source_env entry frame", `"$@"`, resolved, entryFrames, true},
		{"no parent proof", good, "", nil, false},
		{"parent proof for different binary", `eval "$("` + other + `" stdlib)" && __main__ source_env ` + envrc, resolved, nil, false},
		{"trailing command", good + ` && rm -rf /`, resolved, nil, false},
		{"semicolon command", good + `; id`, resolved, nil, false},
		{"pipe", good + ` | sh`, resolved, nil, false},
		{"redirect", good + ` > /tmp/x`, resolved, nil, false},
		{"background", good + ` &`, resolved, nil, false},
		{"or instead of and", `eval "$("` + link + `" stdlib)" || __main__ source_env ` + envrc, resolved, nil, false},
		{"stdlib extra argument", `eval "$("` + link + `" stdlib x)" && __main__ source_env ` + envrc, resolved, nil, false},
		{"other subcommand", `eval "$("` + link + `" export bash)" && __main__ source_env ` + envrc, resolved, nil, false},
		{"other function", `eval "$("` + link + `" stdlib)" && __main__ rm ` + envrc, resolved, nil, false},
		{"extra source_env argument", good + ` extra`, resolved, nil, false},
		{"variable path", `eval "$("` + link + `" stdlib)" && __main__ source_env "$HOME/.envrc"`, resolved, nil, false},
		{"command substitution path", `eval "$("` + link + `" stdlib)" && __main__ source_env "$(id)"`, resolved, nil, false},
		{"relative path", `eval "$("` + link + `" stdlib)" && __main__ source_env .envrc`, resolved, nil, false},
		{"variable direnv", `eval "$("$D" stdlib)" && __main__ source_env ` + envrc, resolved, nil, false},
		{"eval other", `eval "$(curl x)" && __main__ source_env ` + envrc, resolved, nil, false},
		{"eval backticks", "eval \"`" + link + " stdlib`\" && __main__ source_env " + envrc, resolved, nil, false},
		{"multiline", good + "\nid", resolved, nil, false},
		{"at without frames", `"$@"`, resolved, nil, false},
		{"at in envrc function", `"$@"`, resolved, map[string]map[int]string{"FUNCNAME": {0: "_ikesu_guard_debug", 1: "_ikesu_guard_dispatch", 2: "f", 3: "source", 4: "source_env", 5: "__main__"}}, false},
		{"at in envrc top level", `"$@"`, resolved, map[string]map[int]string{"FUNCNAME": {0: "_ikesu_guard_debug", 1: "_ikesu_guard_dispatch", 2: "source", 3: "source_env", 4: "__main__"}}, false},
		{"at with user main", `"$@"`, resolved, map[string]map[int]string{"FUNCNAME": {0: "_ikesu_guard_debug", 1: "_ikesu_guard_dispatch", 2: "__main__", 3: "main"}}, false},
		{"at with extra word", `"$@" x`, resolved, mainFrames, false},
		{"unquoted at", `$@`, resolved, mainFrames, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := Input{shellArrays: tc.arrays, ShellParentDirenv: func() string { return tc.parent }}
			if got := trustedDirenvLine(i, tc.line); got != tc.want {
				t.Fatalf("got %v want %v for %q", got, tc.want, tc.line)
			}
		})
	}
	if trustedDirenvLine(Input{}, good) {
		t.Fatal("trusted without a parent check")
	}
}

func TestShellParentDirenvRejectsBadShellPID(t *testing.T) {
	t.Parallel()
	for _, pid := range []string{"x", "0", "1", "-5", "999999999"} {
		if got := ShellParentDirenv(pid); got != "" {
			t.Errorf("%q: got %q", pid, got)
		}
	}
	// 親(go test)はdirenvではない。
	if got := ShellParentDirenv(""); got != "" {
		t.Errorf("got %q", got)
	}
}
