package machine

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRemoteDirectoryAndLiteralArguments(t *testing.T) {
	for _, v := range []struct{ cwd, home, override, want string }{
		{"/home/local/work", "/home/local", "", `"$HOME"/'work'`},
		{"/home/local", "/home/local", "", `"$HOME"`},
		{"/outside/work", "/home/local", "", `'/outside/work'`},
		{"/x", "/h", "~", `"$HOME"`}, {"/x", "/h", "~/a b", `"$HOME"/'a b'`},
		{"/x", "/h", "relative", `"$HOME"/'relative'`}, {"/x", "/h", "/abs", `'/abs'`},
	} {
		if got := RemoteDirectory(v.cwd, v.home, v.override); got != v.want {
			t.Fatalf("%+v got %s", v, got)
		}
	}
	argv := []string{"printf", "%s\\n", "a'b", "$HOME; false", "", "line\nbreak", "é"}
	a := RunArgs(Profile{Target: "host"}, Quote(t.TempDir()), argv, false)
	// Execute the outer and login shell locally to verify both quote layers,
	// using the real shell; no SSH stub or synthetic remote values.
	out, err := exec.Command("/bin/sh", "-c", a[len(a)-1]).Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Join(argv[2:], "\n") + "\n"; string(out) != want {
		t.Fatalf("got %q want %q", out, want)
	}
	if a[0] != "-n" || RunArgs(Profile{Target: "h"}, "'/'", argv, true)[0] != "-t" {
		t.Fatal(a)
	}
}

func TestSyncPlanNoDeleteAndIgnoredPaths(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n*.tmp\n"), 0600)
	os.Mkdir(filepath.Join(root, "build"), 0700)
	os.WriteFile(filepath.Join(root, "build", "secret"), []byte("x"), 0600)
	sub := filepath.Join(root, "sub")
	os.Mkdir(sub, 0700)
	steps, err := SyncPlan(Profile{Target: "host"}, sub, `"$HOME"/'scratch/sub'`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(steps[0][len(steps[0])-1], "/..") || steps[0][0] != "ssh" || steps[1][0] != "rsync" {
		t.Fatal(steps)
	}
	args := strings.Join(steps[1], "\n")
	for _, want := range []string{"--exclude=/.git", "--exclude=/build/secret", "--exclude=build/", "--exclude=*.tmp"} {
		if !strings.Contains(args, want) {
			t.Fatal(want, args)
		}
	}
	if strings.Contains(args, "--delete") {
		t.Fatal(args)
	}
	unmanaged, err := SyncPlan(Profile{Target: "host"}, t.TempDir(), "'/remote'")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(unmanaged[1][:2], []string{"rsync", "-a"}) {
		t.Fatal(unmanaged)
	}
}
