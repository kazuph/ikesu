package distribution

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/plugin"
)

// Exercise the installer's activation boundary with the actual CLI/server and
// actual ensure implementation. Signature validation is a separate earlier gate.
func TestEnsureRunFailureRollsBackActivation(t *testing.T) {
	node := fixtureNode(t)
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	work, err := os.MkdirTemp("/tmp", "ikesu-ensure-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(work)
	// Node resolves module URLs through /tmp's macOS symlink. Register the
	// same physical path so ensure checks the intended installed version.
	work, err = filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(work, "ikesu")
	buildEnv := fixtureEnvironment(os.Getenv("HOME"), "TMPDIR="+work)
	for _, key := range []string{"GOTOOLCHAIN", "GOMODCACHE", "GOPATH", "GOCACHE", "GOROOT", "CGO_ENABLED"} {
		if value, ok := os.LookupEnv(key); ok {
			buildEnv = append(buildEnv, key+"="+value)
		}
	}
	build := fixtureCommand(t, buildEnv, "go", "build", "-o", binary, "./cmd/ikesu")
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
			env := fixtureEnvironment(home, "IKESU_CONFIG="+config, "IKESU_STATE_DIR="+filepath.Join(home, "state"), "IKESU_SOCKET_PATH="+socket, "IKESU_SOCKET_PATH_EXPLICIT=1", "IKESU_PANE_ID=", "SHELL=/bin/sh")
			logFile, err := os.Create(filepath.Join(home, "server.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer logFile.Close()
			serverCtx, cancelServer := context.WithTimeout(context.Background(), 12*fixtureWait)
			defer cancelServer()
			server := exec.CommandContext(serverCtx, binary, "server")
			server.Env, server.Stdout, server.Stderr = env, logFile, logFile
			server.WaitDelay = fixtureWait
			if err := server.Start(); err != nil {
				t.Fatal(err)
			}
			exited := make(chan struct{})
			var serverErr error
			go func() { serverErr = server.Wait(); close(exited) }()
			cli := func(args ...string) ([]byte, error) {
				return fixtureCommand(t, env, append([]string{binary}, args...)...).CombinedOutput()
			}
			var stopOnce sync.Once
			var stopErr error
			stop := func() error {
				stopOnce.Do(func() {
					select {
					case <-exited:
						stopErr = serverErr
						return
					default:
					}
					output, err := cli("server", "stop")
					if err != nil {
						stopErr = fmt.Errorf("server.stop: %w: %s", err, output)
						cancelServer()
					}
					select {
					case <-exited:
					case <-time.After(fixtureWait):
						stopErr = fmt.Errorf("server exit deadline")
						cancelServer()
						select {
						case <-exited:
						case <-time.After(fixtureWait):
							stopErr = fmt.Errorf("server reaping deadline")
						}
					}
				})
				return stopErr
			}
			defer func() {
				if err := stop(); err != nil {
					t.Error(err)
				}
			}()
			deadline := time.NewTimer(fixtureWait)
			defer deadline.Stop()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				if _, err := os.Stat(socket); err == nil {
					break
				}
				select {
				case <-exited:
					data, _ := os.ReadFile(logFile.Name())
					t.Fatalf("server exited: %v: %s", serverErr, data)
				case <-deadline.C:
					data, _ := os.ReadFile(logFile.Name())
					t.Fatalf("server socket deadline: %s", data)
				case <-ticker.C:
				}
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
			if len([]byte(proxyPath)) >= 104 || len([]byte(socket)) >= 104 {
				t.Fatal("fixture exceeds macOS Unix socket path limit", proxyPath, socket)
			}
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: proxyPath, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			proxyDone := make(chan error, 1)
			relay := func() {
				defer listener.Close()
				for {
					listener.SetDeadline(time.Now().Add(fixtureWait))
					client, err := listener.Accept()
					if err != nil {
						proxyDone <- err
						return
					}
					client.SetDeadline(time.Now().Add(fixtureWait))
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
						err := stop()
						client.Close()
						proxyDone <- err
						return
					}
					upstream, err := net.DialTimeout("unix", socket, fixtureWait)
					if err != nil {
						client.Close()
						proxyDone <- err
						return
					}
					upstream.SetDeadline(time.Now().Add(fixtureWait))
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
			}
			concurrentApplied := false
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
				// An import fixture must not look like direct script invocation:
				// on macOS that would also start the module's AppKit main().
				js := "const {ensureDaemon,loadConfig}=await import(process.argv[2]); await ensureDaemon(loadConfig(process.env),process.env)"
				// Keep the fixture environment and only the engine's plugin fields.
				pluginEnv := []string{}
				for _, pair := range childEnv {
					key, _, _ := strings.Cut(pair, "=")
					if strings.HasPrefix(key, "IKESU_PLUGIN_") || key == "IKESU_SOCKET_PATH" || key == "IKESU_BIN_PATH" || key == "IKESU_CONFIG_PATH" || key == "IKESU_ENV" {
						pluginEnv = append(pluginEnv, pair)
					}
				}
				childEnv = fixtureEnvironment(home, append(env, pluginEnv...)...)
				cmd := fixtureCommand(t, childEnv, node, "--input-type=module", "-e", js, "activation-test", module)
				// Start only when Node needs the relay, after plugin.link completes.
				go relay()
				output, runErr := cmd.CombinedOutput()
				select {
				case err := <-proxyDone:
					if err != nil {
						return true, fmt.Errorf("run.start not observed: %w; ensure: %v: %s", err, runErr, output)
					}
				case <-time.After(fixtureWait):
					return true, fmt.Errorf("proxy completion deadline; ensure: %v: %s", runErr, output)
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
				concurrentApplied = true
				if runErr != nil {
					return true, fmt.Errorf("ensure: %w: %s", runErr, output)
				}
				return true, nil
			}
			err = installer.activate(filepath.Join(root, "new"), "new", false)
			if !concurrentApplied || err == nil || !strings.Contains(err.Error(), "ikesu run に失敗") {
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
