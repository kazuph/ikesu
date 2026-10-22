package distribution

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/plugin"
)

// The helper runs the real installer in a separate process so real OS signals
// exercise cancellation while a saved registration has no response yet.
func TestInstallSignalProcess(t *testing.T) {
	if os.Getenv("IKESU_INSTALL_SIGNAL_HELPER") != "1" {
		return
	}
	home := os.Getenv("IKESU_INSTALL_SIGNAL_HOME")
	cert, err := os.ReadFile(os.Getenv("IKESU_INSTALL_SIGNAL_CERT"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(cert)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	service := &httptest.Server{URL: os.Getenv("IKESU_INSTALL_SIGNAL_URL")}
	serviceClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	// httptest's exported Client uses its configured transport. A downloader with
	// this real local TLS client avoids trusting an unrelated system certificate.
	fetch := signalReleaseDownload{url: service.URL, client: serviceClient}
	store := &plugin.Store{ConfigRoot: filepath.Join(home, "config"), StateRoot: filepath.Join(home, "state"), Version: "0.4.1"}
	i := &Installer{Product: TerminalBrowser, Home: home, Store: store, Fetch: fetch}
	i.Register = func(_ context.Context, manifest string) (bool, error) {
		if _, err := store.Link(manifest, true, nil); err != nil {
			return true, err
		}
		fmt.Println("REGISTRATION_SAVED")
		var release [1]byte
		if _, err := os.Stdin.Read(release[:]); err != nil {
			return true, err
		}
		return true, fmt.Errorf("response lost after saved registration")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	base := "https://github.com/kazuph/herdr/releases/download/kazuph-tb-v0.7.3/"
	release := Release{Tag: "kazuph-tb-v0.7.3", Assets: []Asset{{Name: TerminalBrowser.Asset, URL: base + TerminalBrowser.Asset}, {Name: "SHA256SUMS", URL: base + "SHA256SUMS"}}}
	if _, err := i.Install(ctx, release, false); err == nil || !strings.Contains(err.Error(), "response lost") {
		t.Fatal("missing response-loss failure", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("real signal did not cancel the preparation context")
	}
	if entry, err := store.Registration("fixture.terminal-browser"); err != nil || entry != nil {
		t.Fatal("signal left saved registration", entry, err)
	}
	for _, name := range []string{filepath.Join(i.Directory(), "current-version"), filepath.Join(home, ".local/bin/tb")} {
		if _, err := os.Lstat(name); !os.IsNotExist(err) {
			t.Fatal("signal left activated file", name, err)
		}
	}
	fmt.Println("ROLLBACK_COMPLETE")
}

type signalReleaseDownload struct {
	url    string
	client *http.Client
}

func (f signalReleaseDownload) Fetch(ctx context.Context, url, destination string) error {
	request, err := http.NewRequestWithContext(ctx, "GET", f.url+"/"+filepath.Base(url), nil)
	if err != nil {
		return err
	}
	response, err := f.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("fixture release HTTP %d", response.StatusCode)
	}
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.ReadFrom(response.Body)
	return err
}

func TestRealSignalsCompleteRegistrationRollback(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			fixture, release := fixtureInstaller(t)
			// Use the existing package service, adding basename routes via its TLS client.
			source := fixture.Fetch.(fixtureReleaseService).server
			service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request, err := http.NewRequestWithContext(r.Context(), "GET", source.URL+"/kazuph/herdr/releases/download/"+release.Tag+r.URL.Path, nil)
				if err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				response, err := source.Client().Do(request)
				if err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				defer response.Body.Close()
				w.WriteHeader(response.StatusCode)
				if _, err := io.Copy(w, response.Body); err != nil {
					t.Error(err)
				}
			}))
			defer service.Close()
			certificate := filepath.Join(t.TempDir(), "local-service.der")
			if err := os.WriteFile(certificate, service.Certificate().Raw, 0600); err != nil {
				t.Fatal(err)
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			command := fixtureCommand(t, fixtureEnvironment(fixture.Home,
				"IKESU_INSTALL_SIGNAL_HELPER=1", "IKESU_INSTALL_SIGNAL_HOME="+fixture.Home,
				"IKESU_INSTALL_SIGNAL_URL="+service.URL, "IKESU_INSTALL_SIGNAL_CERT="+certificate),
				exe, "-test.run=^TestInstallSignalProcess$", "-test.v")
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			command.Stderr = command.Stdout
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				scanner := bufio.NewScanner(output)
				for scanner.Scan() {
					line := scanner.Text()
					t.Log(line)
					if line == "REGISTRATION_SAVED" {
						if err := command.Process.Signal(sig); err != nil {
							done <- err
							return
						}
						// Wait for signal delivery by asking the helper to continue only after
						// its context cancellation; the helper observes cancellation before return.
						if _, err := input.Write([]byte{1}); err != nil {
							done <- err
							return
						}
					}
				}
				done <- command.Wait()
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(15 * time.Second):
				command.Process.Kill()
				t.Fatal("signal interrupted rollback or installer hung")
			}
		})
	}
}
