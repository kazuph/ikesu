package distribution

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// A full compile is not a fixture process: under the shared full-suite load
// it can exceed the fixture's process deadline. Build once per package run
// with a deadline sized for compilation and copy the result into each fixture.
const ikesuBuildDeadline = 3 * time.Minute

var ikesuBuild struct {
	once sync.Once
	dir  string
	path string
	out  []byte
	err  error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if ikesuBuild.dir != "" {
		os.RemoveAll(ikesuBuild.dir)
	}
	os.Exit(code)
}

// builtIkesu copies the package-wide build of ./cmd/ikesu to dest.
func builtIkesu(t *testing.T, dest string) {
	t.Helper()
	ikesuBuild.once.Do(func() {
		repository, err := filepath.Abs("../..")
		if err != nil {
			ikesuBuild.err = err
			return
		}
		dir, err := os.MkdirTemp("/tmp", "ikesu-dist-build-")
		if err != nil {
			ikesuBuild.err = err
			return
		}
		ikesuBuild.dir = dir
		ikesuBuild.path = filepath.Join(dir, "ikesu")
		env := fixtureEnvironment(os.Getenv("HOME"), "TMPDIR="+dir)
		for _, key := range []string{"GOTOOLCHAIN", "GOMODCACHE", "GOPATH", "GOCACHE", "GOROOT", "CGO_ENABLED"} {
			if value, ok := os.LookupEnv(key); ok {
				env = append(env, key+"="+value)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), ikesuBuildDeadline)
		defer cancel()
		build := exec.CommandContext(ctx, "go", "build", "-o", ikesuBuild.path, "./cmd/ikesu")
		build.Dir = repository
		build.Env = env
		ikesuBuild.out, ikesuBuild.err = build.CombinedOutput()
	})
	if ikesuBuild.err != nil {
		t.Fatalf("build: %v: %s", ikesuBuild.err, ikesuBuild.out)
	}
	src, err := os.Open(ikesuBuild.path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
}
