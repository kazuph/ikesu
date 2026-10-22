package distribution

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/plugin"
)

// fixtureReleaseService serves real tar.gz files and checksums over local TLS.
// Its download adapter keeps the production GitHub URL validation in use.
type fixtureReleaseService struct{ server *httptest.Server }

func (f fixtureReleaseService) Fetch(ctx context.Context, url, destination string) error {
	at := strings.Index(url, "/repos/")
	if at < 0 {
		at = strings.Index(url, "/kazuph/")
	}
	if at < 0 {
		return fmt.Errorf("unsupported fixture route %s", url)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, f.server.URL+url[at:], nil)
	if err != nil {
		return err
	}
	response, err := f.server.Client().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("release service HTTP %d", response.StatusCode)
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

func fixturePackage(t *testing.T, v, extra string) []byte {
	t.Helper()
	file := filepath.Join(t.TempDir(), "release.tar.gz")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	manifest := `id="fixture.terminal-browser"
name="Terminal Browser"
version="0.1.0"
min_ikesu_version="0.1.0"
platforms=["macos"]
[[actions]]
id="open"
title="Open"
command=["../bin/terminal-browser"]
`
	for _, entry := range []struct {
		name, data string
		mode       int64
	}{
		{"VERSION", v + "\n", 0644}, {"bin/terminal-browser", "#!/bin/sh\nprintf '%s\\n' '" + v + "'\n", 0755},
		{"ikesu-plugin/ikesu-plugin.toml", manifest, 0644}, {"fixture", extra, 0644},
	} {
		h := tar.Header{Name: "terminal-browser/" + entry.name, Typeflag: tar.TypeReg, Size: int64(len(entry.data)), Mode: entry.mode}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixtureInstaller(t *testing.T) (*Installer, Release) {
	t.Helper()
	archive := fixturePackage(t, "0.7.3", "first")
	digest := sha256.Sum256(archive)
	p := TerminalBrowser
	r := Release{Tag: p.TagPrefix + "0.7.3", Assets: []Asset{
		{Name: p.Asset, URL: "https://github.com/" + p.Repository + "/releases/download/" + p.TagPrefix + "0.7.3/" + p.Asset},
		{Name: "SHA256SUMS", URL: "https://github.com/" + p.Repository + "/releases/download/" + p.TagPrefix + "0.7.3/SHA256SUMS"},
	}}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases"):
			json.NewEncoder(w).Encode([]Release{r})
		case strings.HasSuffix(request.URL.Path, "/SHA256SUMS"):
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(digest[:]), p.Asset)
		case strings.HasSuffix(request.URL.Path, "/"+p.Asset):
			w.Write(archive)
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(server.Close)
	home := t.TempDir()
	store := &plugin.Store{ConfigRoot: filepath.Join(home, ".config", "ikesu"), StateRoot: filepath.Join(home, "state"), Version: "0.4.1"}
	return &Installer{Product: p, Home: home, Store: store, Fetch: fixtureReleaseService{server: server}}, r
}

func TestFixtureReleaseDownloadInstallAndWrapperOwnership(t *testing.T) {
	i, r := fixtureInstaller(t)
	selected, err := i.Product.Select(context.Background(), i.Fetch, "https://api.github.com", "")
	if err != nil || selected.Tag != r.Tag {
		t.Fatal(selected, err)
	}
	wrapper := filepath.Join(i.Home, ".local", "bin", "tb")
	if err := os.MkdirAll(filepath.Dir(wrapper), 0755); err != nil {
		t.Fatal(err)
	}
	herdr := []byte("#!/bin/sh\nexec '/herdr/terminal-browser/0.7.2/bin/terminal-browser' \"$@\"\n")
	if err := os.WriteFile(wrapper, herdr, 0755); err != nil {
		t.Fatal(err)
	}
	installed, err := i.Install(context.Background(), r, false)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(wrapper)
	if string(data) != string(herdr) {
		t.Fatal("foreign wrapper overwritten")
	}
	s, err := i.Status()
	if err != nil || !s.WrapperMismatch || s.Current != "0.7.3" || s.Registered != filepath.Join(installed, i.Product.Manifest) {
		t.Fatal(s, err)
	}
	if _, err := i.Install(context.Background(), r, true); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(wrapper)
	if string(data) != string(wrapperBytes(installed)) {
		t.Fatal(string(data))
	}
	if _, err := i.Install(context.Background(), r, false); err != nil {
		t.Fatal("idempotent reinstallation", err)
	}
}

func TestRegistrationFailureRestoresFilesAndPreservesConcurrentOperations(t *testing.T) {
	for _, operation := range []string{"saved-response-lost", "before-send", "link", "disable", "unlink"} {
		t.Run(operation, func(t *testing.T) {
			i, r := fixtureInstaller(t)
			old := filepath.Join(i.Directory(), "0.7.2")
			if err := os.MkdirAll(old, 0755); err != nil {
				t.Fatal(err)
			}
			current := filepath.Join(i.Directory(), "current-version")
			if err := os.WriteFile(current, []byte("0.7.2\n"), 0600); err != nil {
				t.Fatal(err)
			}
			wrapper := filepath.Join(i.Home, ".local", "bin", "tb")
			if err := atomicFile(wrapper, wrapperBytes(old), 0755); err != nil {
				t.Fatal(err)
			}
			var concurrent *plugin.Installed
			i.Register = func(_ context.Context, manifest string) (bool, error) {
				if operation == "before-send" {
					return false, fmt.Errorf("connection unavailable before send")
				}
				installed, err := i.Store.Link(manifest, true, nil)
				if err != nil {
					return true, err
				}
				switch operation {
				case "disable":
					_, err = i.Store.SetEnabled(installed.ID, false)
				case "unlink":
					_, err = i.Store.Unlink(installed.ID)
				case "link":
					other := t.TempDir()
					data, _ := os.ReadFile(manifest)
					if err = os.WriteFile(filepath.Join(other, "ikesu-plugin.toml"), data, 0600); err == nil {
						_, err = i.Store.Link(other, true, nil)
					}
				}
				if err != nil {
					return true, err
				}
				concurrent, err = i.Store.Registration(installed.ID)
				if err != nil {
					return true, err
				}
				return true, fmt.Errorf("response lost after save")
			}
			if _, err := i.Install(context.Background(), r, false); err == nil {
				t.Fatal("failed registration reported success")
			}
			data, _ := os.ReadFile(current)
			if string(data) != "0.7.2\n" {
				t.Fatal("current version not restored", string(data))
			}
			data, _ = os.ReadFile(wrapper)
			if string(data) != string(wrapperBytes(old)) {
				t.Fatal("wrapper not restored", string(data))
			}
			got, err := i.Store.Registration("fixture.terminal-browser")
			if err != nil {
				t.Fatal(err)
			}
			if operation == "saved-response-lost" || operation == "before-send" || operation == "unlink" {
				if got != nil {
					t.Fatal("registration rollback did not remove own write", got)
				}
			} else if got == nil || concurrent == nil || got.ManifestPath != concurrent.ManifestPath || got.Enabled != concurrent.Enabled {
				t.Fatal("concurrent registry update lost", got, concurrent)
			}
		})
	}
}
