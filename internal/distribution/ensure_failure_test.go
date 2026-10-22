package distribution

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/plugin"
)

// Exercise the installer's activation boundary with the actual CLI/server and
// actual ensure implementation. Signature validation is a separate earlier gate.
func TestEnsureRunFailureRollsBackActivation(t *testing.T) {
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	work, err := os.MkdirTemp("/tmp", "ikesu-ensure-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(work)
	binary := filepath.Join(work, "ikesu")
	build := exec.Command("go", "build", "-o", binary, "./cmd/ikesu")
	build.Dir = repository
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	for _, concurrent := range []string{"none", "link", "disable", "unlink"} {
		t.Run(concurrent, func(t *testing.T) {
			home := filepath.Join(work, concurrent)
			if err := os.MkdirAll(home, 0700); err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(home, "config.toml")
			if err := os.WriteFile(config, []byte("[experimental]\nallow_nested=true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			socket := filepath.Join(home, "state", "default", "ikesu.sock")
			env := append(os.Environ(), "HOME="+home, "IKESU_CONFIG="+config, "IKESU_STATE_DIR="+filepath.Join(home, "state"), "IKESU_SOCKET_PATH="+socket, "IKESU_SOCKET_PATH_EXPLICIT=1", "IKESU_PANE_ID=", "SHELL=/bin/sh")
			server := exec.Command(binary, "server")
			server.Env = env
			if err := server.Start(); err != nil {
				t.Fatal(err)
			}
			exited := make(chan error, 1)
			go func() { exited <- server.Wait() }()
			stopped := false
			cli := func(args ...string) ([]byte, error) {
				cmd := exec.Command(binary, args...)
				cmd.Env = env
				return cmd.CombinedOutput()
			}
			stop := func() {
				if !stopped {
					_, _ = cli("server", "stop")
					<-exited
					stopped = true
				}
			}
			defer stop()
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(socket); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("server socket deadline")
				}
				time.Sleep(10 * time.Millisecond)
			}
			store := &plugin.Store{ConfigRoot: home, StateRoot: filepath.Join(home, "state"), Version: "0.4.1"}
			installer := &Installer{Product: MacDialog, Home: home, Store: store}
			root := installer.Directory()
			for _, version := range []string{"old", "new", "other"} {
				dir := filepath.Join(root, version)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				files, err := filepath.Glob(filepath.Join(repository, "apps/mac-dialog-decisions", "*.mjs"))
				if err != nil {
					t.Fatal(err)
				}
				files = append(files, filepath.Join(repository, "apps/mac-dialog-decisions/ikesu-plugin.toml"))
				for _, source := range files {
					data, err := os.ReadFile(source)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, filepath.Base(source)), data, 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			manifest := func(version string) string { return filepath.Join(root, version, "ikesu-plugin.toml") }
			if _, err := store.Link(manifest("old"), true, nil); err != nil {
				t.Fatal(err)
			}
			current := filepath.Join(root, "current-version")
			if err := os.WriteFile(current, []byte("old\n"), 0600); err != nil {
				t.Fatal(err)
			}
			proxyPath := filepath.Join(home, "proxy.sock")
			listener, err := net.Listen("unix", proxyPath)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			proxyDone := make(chan error, 1)
			go func() {
				defer listener.Close()
				for {
					client, err := listener.Accept()
					if err != nil {
						proxyDone <- err
						return
					}
					request, err := bufio.NewReader(client).ReadBytes('\n')
					if err != nil {
						client.Close()
						proxyDone <- err
						return
					}
					var decoded api.Request
					if err := json.Unmarshal(request, &decoded); err != nil {
						client.Close()
						proxyDone <- err
						return
					}
					if decoded.Method == api.MethodRunStart {
						// All preceding replies came from the live server. Stop it
						// only after the real CLI has sent its run.start request.
						stop()
						client.Close()
						proxyDone <- nil
						return
					}
					upstream, err := net.Dial("unix", socket)
					if err != nil {
						client.Close()
						proxyDone <- err
						return
					}
					_, err = upstream.Write(request)
					if err != nil {
						upstream.Close()
						client.Close()
						proxyDone <- err
						return
					}
					reply, err := bufio.NewReader(upstream).ReadBytes('\n')
					upstream.Close()
					if err == nil {
						_, err = client.Write(reply)
					}
					client.Close()
					if err != nil {
						proxyDone <- err
						return
					}
				}
			}()
			installer.Register = func(_ context.Context, saved string) (bool, error) {
				if out, err := cli("plugin", "link", saved); err != nil {
					return true, fmt.Errorf("register: %w: %s", err, out)
				}
				entry, err := store.PreparedLink(saved, true, nil)
				if err != nil {
					return true, err
				}
				engine := plugin.Engine{Store: store, SocketPath: proxyPath, BinPath: binary, ConfigPath: config}
				childEnv, err := engine.Environment(entry, plugin.Context{}, nil)
				if err != nil {
					return true, err
				}
				module := filepath.Join(root, "new/mac-dialog-decisions.mjs")
				js := "const {ensureDaemon,loadConfig}=await import(process.argv[1]); await ensureDaemon(loadConfig(process.env),process.env)"
				cmd := exec.Command("node", "--input-type=module", "-e", js, module)
				cmd.Env = childEnv
				output, runErr := cmd.CombinedOutput()
				if err := <-proxyDone; err != nil && err != io.EOF {
					return true, err
				}
				switch concurrent {
				case "link":
					_, err = store.Link(manifest("other"), true, nil)
				case "disable":
					_, err = store.SetEnabled(entry.ID, false)
				case "unlink":
					_, err = store.Unlink(entry.ID)
				}
				if err != nil {
					return true, err
				}
				if runErr != nil {
					return true, fmt.Errorf("ensure: %w: %s", runErr, output)
				}
				return true, nil
			}
			err = installer.activate(filepath.Join(root, "new"), "new", false)
			if err == nil || !strings.Contains(err.Error(), "ikesu run に失敗") {
				t.Fatalf("run failure must fail activation: %v", err)
			}
			if data, err := os.ReadFile(current); err != nil || string(data) != "old\n" {
				t.Fatalf("current-version rollback: %q %v", data, err)
			}
			entry, err := store.Registration("kazuph.mac-dialog-decisions")
			if err != nil {
				t.Fatal(err)
			}
			switch concurrent {
			case "none":
				if entry == nil || entry.Root != filepath.Join(root, "old") {
					t.Fatal("old registration not restored", entry)
				}
			case "link":
				if entry == nil || entry.Root != filepath.Join(root, "other") {
					t.Fatal("other link lost", entry)
				}
			case "disable":
				if entry == nil || entry.Enabled {
					t.Fatal("disable lost", entry)
				}
			case "unlink":
				if entry != nil {
					t.Fatal("unlink lost", entry)
				}
			}
		})
	}
}
