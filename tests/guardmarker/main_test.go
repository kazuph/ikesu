package guardmarker

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/childenv"
)

var realGuardSocket string

// fixtureBinary is built once in TestMain; tests only execute it.
var fixtureBinary string

// The guard now requires identity from a real server even for safe commands.
// This isolated server is shared by the package's independent asset fixtures.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("/tmp", "ikesu-guard-fixture-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary := filepath.Join(root, "ikesu")
	fixtureBinary = binary
	build := exec.Command("go", "build", "-o", binary, "../../cmd/ikesu")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, err, string(out))
		_ = os.RemoveAll(root)
		os.Exit(1)
	}
	cfg := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n"), 0600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(root)
		os.Exit(1)
	}
	server := exec.Command(binary, "--session", "fixture", "server")
	server.Env = append(childenv.Scrub(os.Environ()), "HOME="+root, "SHELL=/bin/sh", "IKESU_STATE_DIR="+root, "IKESU_CONFIG="+cfg)
	var diagnostic bytes.Buffer
	server.Stdout, server.Stderr = &diagnostic, &diagnostic
	if err := server.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(root)
		os.Exit(1)
	}
	realGuardSocket = api.APISocketPath(filepath.Join(root, "fixture"))
	deadline := time.Now().Add(5 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", realGuardSocket, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	code := 1
	if ready {
		code = m.Run()
	} else {
		fmt.Fprintln(os.Stderr, "isolated guard server did not start:", diagnostic.String())
	}
	if conn, err := net.DialTimeout("unix", realGuardSocket, time.Second); err == nil {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_ = api.WriteRequest(conn, api.Request{ID: "cleanup", Method: api.MethodServerStop, Params: api.MarshalParams(nil)})
		_, _ = io.Copy(io.Discard, conn)
		_ = conn.Close()
	}
	if err := server.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, "isolated server:", err)
		code = 1
	}
	if err := os.RemoveAll(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}
