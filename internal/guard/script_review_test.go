package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScriptFileReviewReproductions(t *testing.T) {
	i, _ := scriptPane(t)
	i.Env["TARGET"] = "/tmp/safe"
	i.Env["PWD"] = "/tmp/safe"
	i.Env["OLDPWD"] = "/tmp/safe"
	for _, tc := range []struct{ name, body string }{
		{"privileged-inline", `bash -p -c 'read TARGET; /bin/rm "$TARGET"'`},
		{"builtin-read", `builtin read TARGET; /bin/rm "$TARGET"`},
		{"command-read", `command read TARGET; /bin/rm "$TARGET"`},
		{"prefixed-unset", `command builtin unset TARGET; /bin/rm "${TARGET:-/outside/victim}"`},
		{"cd-pwd", `cd /; /bin/rm "$PWD/victim"`},
		{"quoted-cd-pwd", `command builtin "cd" /; /bin/rm "$PWD/victim"`},
		{"pushd-oldpwd", `pushd /; /bin/rm "$OLDPWD/victim"`},
		{"popd-pwd", `popd; /bin/rm "$PWD/victim"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(i.Cwd, tc.name+".sh")
			if e := os.WriteFile(path, []byte(tc.body), 0600); e != nil {
				t.Fatal(e)
			}
			_, v, _ := lineVerdict(Config{}, i, "sh "+quote(path))
			if v.Decision != "ask" {
				t.Fatalf("got %+v want preflight ask", v)
			}
		})
	}
}
func TestScriptFileReviewBashIdentity(t *testing.T) {
	i, _ := scriptPane(t)
	script := filepath.Join(i.Cwd, "program.sh")
	os.WriteFile(script, []byte("read TARGET\n/bin/rm \"$TARGET\"\n"), 0600)
	fakeDir := filepath.Join(i.Cwd, "fake")
	os.Mkdir(fakeDir, 0700)
	fake := filepath.Join(fakeDir, "bash")
	for _, kind := range []string{"dash-link", "executable-file"} {
		t.Run(kind, func(t *testing.T) {
			os.Remove(fake)
			if kind == "dash-link" {
				if e := os.Symlink("/bin/dash", fake); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
					t.Fatal(e)
				}
			}
			next := i
			next.Env = EnvMap(environment(i.Env))
			next.Env["PATH"] = fakeDir + ":/usr/bin:/bin"
			for _, command := range []string{"bash", fake} {
				_, v, _ := lineVerdict(Config{}, next, quote(command)+" "+quote(script))
				if v.Decision != "ask" {
					t.Fatalf("%s: got %+v", command, v)
				}
			}
		})
	}
}

func TestScriptFileReviewAdjacentPriority(t *testing.T) {
	i, _ := scriptPane(t)
	pairs := []struct{ name, strong, weak, rule, want string }{
		{"danger-over-unparseable", "rm /outside/victim\n", "source opaque\n", "rm-paths", "ask"},
		{"unparseable-over-unresolved", "source opaque\n", "read TARGET; rm \"$TARGET\"\n", "guard-boundary", "ask"},
		{"unresolved-over-safe", "read TARGET; rm \"$TARGET\"\n", "printf okay\n", "default", "allow"},
	}
	for _, pair := range pairs {
		for _, reverse := range []bool{false, true} {
			first, second := pair.strong, pair.weak
			if reverse {
				first, second = second, first
			}
			path := filepath.Join(i.Cwd, "pair.sh")
			os.WriteFile(path, []byte(first+second), 0600)
			_, v, _ := lineVerdict(Config{}, i, "bash "+quote(path))
			if v.Decision != pair.want || v.Rule != pair.rule {
				t.Fatalf("%s reverse=%v: %+v", pair.name, reverse, v)
			}
			if pair.name == "unresolved-over-safe" {
				next := i
				next.Env = EnvMap(environment(i.Env))
				delete(next.Env, "BASH_ENV")
				_, v, _ = lineVerdict(Config{}, next, "bash "+quote(path))
				if v.Decision != "ask" {
					t.Fatalf("unproven runtime reverse=%v: %+v", reverse, v)
				}
			}
		}
	}
}
func TestScriptFileReviewNestedOptions(t *testing.T) {
	i, _ := scriptPane(t)
	for _, tc := range []struct{ flags, want string }{
		{"-p -c", "ask"}, {"-pc", "ask"}, {"-cp", "ask"}, {"--posix -c", "ask"}, {"-o NO_RCS -c", "ask"}, {"--unknown -c", "ask"},
		{"-ec", "allow"}, {"-o pipefail -c", "allow"}, {"--norc --noprofile -c", "allow"},
	} {
		path := filepath.Join(i.Cwd, "nested.sh")
		os.WriteFile(path, []byte("bash "+tc.flags+" 'read TARGET; rm \"$TARGET\"'\n"), 0600)
		_, v, _ := lineVerdict(Config{}, i, "sh "+quote(path))
		if v.Decision != tc.want {
			t.Fatalf("%s: %+v want %s", tc.flags, v, tc.want)
		}
	}
}
