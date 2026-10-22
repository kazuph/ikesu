package server

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/distribution"
	"github.com/kazuph/ikesu/internal/plugin"
	"github.com/kazuph/ikesu/internal/teststate"
)

type releaseFixtureDownload struct{ service *httptest.Server }

func (f releaseFixtureDownload) Fetch(ctx context.Context, url, destination string) error {
	name := filepath.Base(url)
	req, err := http.NewRequestWithContext(ctx, "GET", f.service.URL+"/"+name, nil)
	if err != nil {
		return err
	}
	response, err := f.service.Client().Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("fixture download status %d", response.StatusCode)
	}
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, response.Body)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func rollbackDistribution(t *testing.T, store *plugin.Store) (*distribution.Installer, distribution.Release) {
	t.Helper()
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	tw := tar.NewWriter(gz)
	manifest := "id=\"fixture.rollback\"\nname=\"Rollback\"\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\nplatforms=[\"macos\"]\n"
	for _, file := range []struct {
		name, value string
		mode        int64
	}{
		{"VERSION", "0.7.3\n", 0644}, {"bin/terminal-browser", "#!/bin/sh\nexit 0\n", 0755}, {"ikesu-plugin/ikesu-plugin.toml", manifest, 0644},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: "terminal-browser/" + file.name, Typeflag: tar.TypeReg, Mode: file.mode, Size: int64(len(file.value))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(file.value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	archive := compressed.Bytes()
	digest := sha256.Sum256(archive)
	service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/SHA256SUMS" {
			fmt.Fprintf(w, "%s  terminal-browser-darwin-arm64.tar.gz\n", hex.EncodeToString(digest[:]))
			return
		}
		if r.URL.Path == "/terminal-browser-darwin-arm64.tar.gz" {
			w.Write(archive)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(service.Close)
	p := distribution.TerminalBrowser
	tag := p.TagPrefix + "0.7.3"
	base := "https://github.com/" + p.Repository + "/releases/download/" + tag + "/"
	return &distribution.Installer{Product: p, Home: t.TempDir(), Store: store, Fetch: releaseFixtureDownload{service}}, distribution.Release{Tag: tag, Assets: []distribution.Asset{{Name: p.Asset, URL: base + p.Asset}, {Name: "SHA256SUMS", URL: base + "SHA256SUMS"}}}
}

func TestDistributionResponseLossRollback(t *testing.T) {
	for _, operation := range []string{"saved-response-lost", "link", "disable", "unlink"} {
		t.Run(operation, func(t *testing.T) {
			teststate.Lock(t)
			dir, done := startPluginServer(t)
			defer stopServer(t, dir, done)
			store := plugin.DefaultStore("0.4.1")
			i, release := rollbackDistribution(t, store)
			proxyDir, err := os.MkdirTemp("/tmp", "ikesu-drop-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(proxyDir) })
			proxyPath := filepath.Join(proxyDir, "drop.sock")
			proxy, err := net.ListenUnix("unix", &net.UnixAddr{Name: proxyPath, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer proxy.Close()
			observed := make(chan error, 1)
			var concurrent *plugin.Installed
			go func() {
				proxy.SetDeadline(time.Now().Add(10 * time.Second))
				client, err := proxy.Accept()
				if err != nil {
					observed <- err
					return
				}
				defer client.Close()
				backend, err := net.DialTimeout("unix", api.APISocketPath(dir), 10*time.Second)
				if err != nil {
					observed <- err
					return
				}
				defer backend.Close()
				client.SetDeadline(time.Now().Add(10 * time.Second))
				backend.SetDeadline(time.Now().Add(10 * time.Second))
				request, err := bufio.NewReader(client).ReadBytes('\n')
				if err != nil {
					observed <- err
					return
				}
				if _, err = backend.Write(request); err != nil {
					observed <- err
					return
				}
				reply, err := bufio.NewReader(backend).ReadBytes('\n')
				if err != nil {
					observed <- err
					return
				}
				var response api.Response
				if json.Unmarshal(reply, &response) != nil || response.Err != nil {
					observed <- fmt.Errorf("backend registration failed: %s", reply)
					return
				}
				registered, err := store.Registration("fixture.rollback")
				if err != nil || registered == nil {
					observed <- fmt.Errorf("backend did not save registration: %v", err)
					return
				}
				if operation != "saved-response-lost" {
					method := "plugin." + operation
					params := map[string]any{"plugin_id": registered.ID}
					if operation == "link" {
						other, err := os.MkdirTemp(i.Home, "other-plugin-")
						if err != nil {
							observed <- err
							return
						}
						data, err := os.ReadFile(registered.ManifestPath)
						if err == nil {
							err = os.WriteFile(filepath.Join(other, "ikesu-plugin.toml"), data, 0600)
						}
						if err != nil {
							observed <- err
							return
						}
						params = map[string]any{"path": other}
					}
					result := apiReq(t, dir, method, params)
					if result.Err != nil {
						observed <- result.Err
						return
					}
					concurrent, err = store.Registration(registered.ID)
					if err != nil {
						observed <- err
						return
					}
				}
				// Close the real client socket after the real backend has saved;
				// no response bytes are forwarded to the installer.
				observed <- nil
			}()
			i.Register = func(ctx context.Context, manifest string) (bool, error) {
				client, err := (&net.Dialer{}).DialContext(ctx, "unix", proxyPath)
				if err != nil {
					return false, err
				}
				defer client.Close()
				client.SetDeadline(time.Now().Add(10 * time.Second))
				if err := api.WriteRequest(client, api.Request{ID: "install", Method: "plugin.link", Params: api.MarshalParams(map[string]any{"path": manifest})}); err != nil {
					return true, err
				}
				_, err = bufio.NewReader(client).ReadBytes('\n')
				return true, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err = i.Install(ctx, release, false)
			if err == nil || !strings.Contains(err.Error(), "EOF") {
				t.Fatalf("expected real lost response, got %v", err)
			}
			select {
			case err := <-observed:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("registration relay completion deadline", ctx.Err())
			}
			got, err := store.Registration("fixture.rollback")
			if err != nil {
				t.Fatal(err)
			}
			if operation == "saved-response-lost" || operation == "unlink" {
				if got != nil {
					t.Fatal("registration rollback did not remove own write", got)
				}
			} else if got == nil || concurrent == nil || got.ManifestPath != concurrent.ManifestPath || got.Enabled != concurrent.Enabled {
				t.Fatal("concurrent operation lost", got, concurrent)
			}
			if _, err := os.Stat(filepath.Join(i.Directory(), "current-version")); !os.IsNotExist(err) {
				t.Fatal("version pointer was not rolled back", err)
			}
			if _, err := os.Stat(filepath.Join(i.Home, ".local", "bin", "tb")); !os.IsNotExist(err) {
				t.Fatal("wrapper was not rolled back", err)
			}
		})
	}
}
