package guard

import (
	"bytes"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScriptFileStaticBranches(t *testing.T) {
	d := t.TempDir()
	i := Input{Cwd: d, LaunchCwd: d, Env: map[string]string{"PATH": "/usr/bin:/bin", "HOME": d, "TARGET": "/tmp/safe"}}
	for _, tc := range []struct{ name, body, want string }{
		{"safe", "if true; then printf ok; fi\nf() { printf hi; }\nf\n", "allow"},
		{"danger", "printf before\nrm /outside/script-test\n", "ask"},
		{"block", "printf before\ngit reset --hard\n", "block"},
		{"parse", "if ; then\n", "ask"},
		{"variable", "read TARGET\nrm \"$TARGET\"\n", "ask"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(d, tc.name+".sh")
			if e := os.WriteFile(path, []byte(tc.body), 0600); e != nil {
				t.Fatal(e)
			}
			_, v, _ := lineVerdict(Config{}, i, "bash "+quote(path))
			if v.Decision != tc.want {
				t.Fatalf("got %+v want %s", v, tc.want)
			}
			if tc.name == "danger" && !strings.Contains(v.Reason, "の2行目:") {
				t.Fatalf("missing source line: %+v", v)
			}
		})
	}
}

func scriptPane(t *testing.T) (Input, *Assets) {
	t.Helper()
	d := t.TempDir()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	a := NewAssets(d, "p_1", exe, Config{})
	env, e := a.Prepare([]string{"HOME=" + d, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + filepath.Join(d, "socket")}, d)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Sync("codex"); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.Close() })
	return Input{Cwd: d, LaunchCwd: d, Env: EnvMap(env)}, a
}
func TestScriptFileInvocationTable(t *testing.T) {
	i, _ := scriptPane(t)
	if e := os.WriteFile(filepath.Join(i.Cwd, "safe.sh"), []byte("printf ok\n"), 0600); e != nil {
		t.Fatal(e)
	}
	cases := []struct{ line, want string }{
		{"bash safe.sh", "allow"}, {"bash -euxv safe.sh", "allow"}, {"bash -o pipefail +o xtrace --norc --noprofile safe.sh", "allow"},
		{"bash safe.sh -c ls", "allow"}, {"bash safe.sh -", "allow"}, {"bash -- safe.sh", "allow"},
		{"bash safe.sh < data", "allow"}, {"cat f | bash safe.sh", "allow"}, {"bash safe.sh 2>&1 | tail", "allow"},
		{"bash safe.sh <<EOF\ndata\nEOF", "allow"},
		{"bash", "ask"}, {"bash -", "ask"}, {"bash -s", "ask"}, {"sh < f", "ask"}, {"curl x | sh", "ask"},
		{"bash /dev/stdin", "ask"}, {"bash /dev/fd/3", "ask"}, {"bash <(printf ok)", "ask"},
		{"bash -i safe.sh", "ask"}, {"bash -p safe.sh", "ask"}, {"bash --posix safe.sh", "ask"}, {"bash --rcfile safe.sh", "ask"},
		{"bash --init-file safe.sh", "ask"}, {"zsh -f safe.sh", "ask"}, {"zsh -o NO_RCS safe.sh", "ask"},
		{"bash -o", "ask"}, {"bash -o bogus safe.sh", "ask"}, {"bash --unknown safe.sh", "ask"},
		{"BASH_ENV=x bash safe.sh", "ask"}, {"ZDOTDIR=x bash safe.sh", "ask"}, {"ENV=x bash safe.sh", "ask"}, {"PATH=x bash safe.sh", "ask"},
		{"env -i bash safe.sh", "ask"}, {"env -u FOO bash safe.sh", "ask"}, {"env PATH=x bash safe.sh", "ask"},
		{"bash -c 'printf ok'", "allow"}, {"bash <<EOF\nprintf ok\nEOF", "allow"}, {"bash -e <<EOF\nprintf ok\nEOF", "allow"}, {"bash -o pipefail <<EOF\nprintf ok\nEOF", "allow"},
		{"source safe.sh", "ask"}, {". safe.sh", "ask"}, {"eval 'printf ok'", "ask"},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			_, v, _ := lineVerdict(Config{}, i, tc.line)
			if v.Decision != tc.want {
				t.Fatalf("got %+v want %s", v, tc.want)
			}
		})
	}
}
func TestScriptFileRuntimeProof(t *testing.T) {
	for _, test := range []string{"valid", "absent", "fake", "other-pane", "unreadable", "record", "claude"} {
		t.Run(test, func(t *testing.T) {
			i, a := scriptPane(t)
			path := filepath.Join(i.Cwd, "variable.sh")
			os.WriteFile(path, []byte("read TARGET\nrm \"$TARGET\"\n"), 0600)
			want := "ask"
			switch test {
			case "valid":
				want = "allow"
			case "absent":
				delete(i.Env, "BASH_ENV")
			case "fake":
				os.WriteFile(i.Env["BASH_ENV"], []byte("# fake\n"), 0600)
			case "other-pane":
				i.Env["BASH_ENV"] = filepath.Join(i.Cwd, "guard", "shell", "other", "bash-env")
			case "unreadable":
				os.Chmod(i.Env["BASH_ENV"], 0000)
			case "record":
				a.Reconfigure(Config{Mode: "record"})
			case "claude":
				i.Env["BASH_ENV"] = "/other/loader"
				i = PrepareClaudeBashEnv(i)
				want = "allow"
			}
			for _, shell := range []string{"bash", "zsh", "sh", "dash", "ksh"} {
				expected := want
				if shell != "bash" {
					expected = "ask"
				}
				_, v, _ := lineVerdict(Config{}, i, shell+" "+quote(path))
				if v.Decision != expected {
					t.Fatalf("%s got %+v want %s", shell, v, expected)
				}
			}
		})
	}
}
func TestScriptFileAggregateAndNested(t *testing.T) {
	i, _ := scriptPane(t)
	write := func(name, body string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(i.Cwd, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("unknown.sh", "read TARGET\nrm \"$TARGET\"\n")
	for _, tc := range []struct{ body, want string }{
		{"rm /outside/victim\nread TARGET\nrm \"$TARGET\"\n", "ask"},
		{"read TARGET\nrm \"$TARGET\"\nrm /outside/victim\n", "ask"},
		{"git reset\nsource unknown\n", "block"}, {"source unknown\ngit reset\n", "block"},
		{"bash unknown.sh\n", "allow"}, {"sh unknown.sh\n", "ask"},
		{"bash root.sh\n", "ask"}, {"cd /tmp\nbash unknown.sh\n", "ask"},
		{"TARGET=/tmp/okay\nrm \"$TARGET\"\n", "allow"},
		{"rm \"$1\"\n", "allow"},
		{"read CMD\n\"$CMD\"\n", "allow"},
	} {
		write("root.sh", tc.body)
		_, v, _ := lineVerdict(Config{}, i, "bash root.sh")
		if v.Decision != tc.want {
			t.Errorf("%q: %+v want %s", tc.body, v, tc.want)
		}
	}
}

func TestScriptFileBoundsAndFailures(t *testing.T) {
	i, _ := scriptPane(t)
	write := func(name, body string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(i.Cwd, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("big.sh", strings.Repeat("#", scriptFileLimit+1))
	write("cycle.sh", "bash second.sh\n")
	write("second.sh", "bash cycle.sh\n")
	write("d1.sh", "bash d2.sh\n")
	write("d2.sh", "bash d3.sh\n")
	write("d3.sh", "bash d4.sh\n")
	write("d4.sh", "printf okay\n")
	write("chunk.sh", strings.Repeat("#", scriptFileLimit-1)+"\n")
	write("total.sh", strings.Repeat("bash chunk.sh\n", 5))
	os.Symlink("/dev/null", filepath.Join(i.Cwd, "special.sh"))
	if e := unix.Mkfifo(filepath.Join(i.Cwd, "fifo.sh"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"missing.sh", "big.sh", "cycle.sh", "d1.sh", "total.sh", "special.sh", "fifo.sh", "."} {
		t.Run(name, func(t *testing.T) {
			_, v, _ := lineVerdict(Config{}, i, "bash "+name)
			if v.Decision != "ask" {
				t.Fatalf("got %+v", v)
			}
		})
	}
	write("d3.sh", "printf okay\n")
	_, v, _ := lineVerdict(Config{}, i, "bash d1.sh")
	if v.Decision != "allow" {
		t.Fatalf("depth=3: %+v", v)
	}
	write("protected.sh", "> .allow-main\n")
	_, v, _ = lineVerdict(Config{}, i, "bash protected.sh")
	if v.Decision != "block" {
		t.Fatalf("redirection: %+v", v)
	}
	write("inline.sh", `sh -c 'read TARGET; rm "$TARGET"'`)
	_, v, _ = lineVerdict(Config{}, i, "bash inline.sh")
	if v.Decision != "ask" {
		t.Fatalf("inner sh: %+v", v)
	}
}

func TestScriptFileAssignedDefaultsAreNotParentValues(t *testing.T) {
	i, _ := scriptPane(t)
	i.Env["TARGET"] = "/tmp/safe"
	for _, body := range []string{`TARGET=/outside/victim; rm "${TARGET:-/tmp/safe}"`, `read TARGET; rm "${TARGET-/tmp/safe}"`, `rm "${1:-/tmp/safe}"`} {
		path := filepath.Join(i.Cwd, "defaults.sh")
		os.WriteFile(path, []byte(body), 0600)
		_, v, _ := lineVerdict(Config{}, i, "sh "+quote(path))
		if v.Decision != "ask" {
			t.Fatalf("%s got %+v", body, v)
		}
	}
}
func TestScriptFileDoesNotUseInnerApprovalKeys(t *testing.T) {
	i, _ := scriptPane(t)
	path := filepath.Join(i.Cwd, "danger.sh")
	os.WriteFile(path, []byte("rm /outside/victim\n"), 0600)
	inner := Evaluate(Config{}, Input{Command: "rm", Args: []string{"/outside/victim"}, Cwd: i.Cwd, Env: i.Env})
	grants := filepath.Join(i.Cwd, "grants")
	if e := WriteApproval(grants, inner.TempKey, time.Now().Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	i.Env["AI_GUARD_TEMP_APPROVAL_FILE"] = grants
	selected, v, _ := lineVerdict(Config{}, i, "bash "+quote(path))
	if v.Decision != "ask" || selected.Command != "bash" {
		t.Fatalf("inner grant suppressed file: %+v %+v", selected, v)
	}
	var out bytes.Buffer
	if AuthorizeLine(Config{}, i.Cwd, i, "bash "+quote(path), &out, nil) {
		t.Fatal("inner grant authorized outer bash")
	}
	// A grant for the outer invocation cannot override a hard block either.
	os.WriteFile(path, []byte("git reset\n"), 0600)
	outer := Evaluate(Config{}, Input{Command: "bash", Args: []string{path}, Cwd: i.Cwd, Env: i.Env})
	if e := WriteApproval(grants, outer.TempKey, time.Now().Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	if AuthorizeLine(Config{}, i.Cwd, i, "bash "+quote(path), &out, nil) {
		t.Fatal("outer grant authorized blocked script")
	}
}

func TestScriptFileAggregateOrder(t *testing.T) {
	i, _ := scriptPane(t)
	// Permutations exercise actual parsing/policy, including unknown expansion
	// and an opaque delegated program. Hard blocks must always win.
	pieces := []string{"git reset\n", "rm /outside/victim\n", "source unparsed\n", "read CMD; \"$CMD\"\n", "printf okay\n"}
	var permutations func([]string, int)
	permutations = func(parts []string, n int) {
		if n == len(parts) {
			path := filepath.Join(i.Cwd, "order.sh")
			os.WriteFile(path, []byte(strings.Join(parts, "")), 0600)
			_, v, _ := lineVerdict(Config{}, i, "bash "+quote(path))
			if v.Decision != "block" {
				t.Fatalf("%q: %+v", parts, v)
			}
			return
		}
		for k := n; k < len(parts); k++ {
			parts[n], parts[k] = parts[k], parts[n]
			permutations(parts, n+1)
			parts[n], parts[k] = parts[k], parts[n]
		}
	}
	permutations(pieces, 0)
	for _, body := range []string{"source opaque\nrm /outside/victim\nread CMD; \"$CMD\"", "read CMD; \"$CMD\"\nrm /outside/victim\nsource opaque"} {
		path := filepath.Join(i.Cwd, "order.sh")
		os.WriteFile(path, []byte(body), 0600)
		_, v, _ := lineVerdict(Config{}, i, "bash "+quote(path))
		if v.Decision != "ask" || v.Rule != "rm-paths" {
			t.Fatalf("%q: %+v", body, v)
		}
	}
}

func TestScriptFileWrappedUnknownCommands(t *testing.T) {
	i, _ := scriptPane(t)
	for _, body := range []string{`read TARGET; command rm "$TARGET"`, `read TARGET; env X=x rm "$TARGET"`} {
		path := filepath.Join(i.Cwd, "wrapped.sh")
		os.WriteFile(path, []byte(body), 0600)
		_, v, _ := lineVerdict(Config{}, i, "bash "+quote(path))
		if v.Decision != "allow" {
			t.Fatalf("%s: %+v", body, v)
		}
	}
}
