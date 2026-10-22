package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/distribution"
	"github.com/kazuph/ikesu/internal/plugin"
)

func (r *runner) distributionCLI(p distribution.Product, args []string) int {
	command := "tb"
	if p.Name == "mac-dialog-decisions" {
		command = "mac-dialog"
	}
	usage := "usage: ikesu " + command + " <install [--version MAJOR.MINOR.PATCH]|status>"
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(r.out(), usage)
		return 0
	}
	if args[0] != "install" && args[0] != "status" {
		return usageErr(r, usage)
	}
	requested, takeOver := "", false
	for n := 1; n < len(args); {
		switch args[n] {
		case "--version":
			if args[0] != "install" {
				return usageErr(r, usage)
			}
			v, ok := flagValue(args, &n)
			if !ok || v == "" {
				return usageErr(r, usage)
			}
			requested = v
		case "--take-over-wrapper":
			if command != "tb" || args[0] != "install" {
				return usageErr(r, usage)
			}
			takeOver = true
			n++
		default:
			return usageErr(r, usage)
		}
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		fmt.Fprintln(r.errOut(), command+" distribution supports macOS Apple silicon only")
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(r.errOut(), err)
		return 1
	}
	version := r.o.Version
	if version == "" {
		version = "0.4.1"
	}
	store := plugin.DefaultStore(version)
	configPath := r.o.getenv("IKESU_CONFIG_PATH")
	if configPath != "" {
		if !filepath.IsAbs(configPath) {
			fmt.Fprintln(r.errOut(), "IKESU_CONFIG_PATH must be absolute")
			return 1
		}
		store.ConfigRoot = filepath.Dir(configPath)
	} else {
		configPath, err = filepath.Abs(config.Path())
		if err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
	}
	installer := &distribution.Installer{Product: p, Home: home, Store: store, Fetch: distribution.Curl{}}
	installer.Register = func(ctx context.Context, manifest string) (bool, error) {
		sent, err := r.registerDistribution(ctx, store, manifest)
		if err != nil || command != "mac-dialog" {
			return sent, err
		}
		p, err := store.PreparedLink(manifest, true, nil)
		if err != nil {
			return true, err
		}
		exe, err := os.Executable()
		if err != nil {
			return true, err
		}
		engine := plugin.Engine{Store: store, SocketPath: r.socketPath(), BinPath: exe, ConfigPath: configPath}
		env, err := engine.Environment(p, plugin.Context{}, nil)
		if err != nil {
			return true, err
		}
		cmd := exec.CommandContext(ctx, "node", filepath.Join(p.Root, "mac-dialog-decisions.mjs"), "ensure")
		cmd.Env = env
		cmd.Dir = p.Root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return true, fmt.Errorf("start or request Mac dialog cutover: %w: %s", err, output)
		}
		return true, nil
	}
	if command == "mac-dialog" {
		installer.Verify = verifyMacDialogSignature
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if args[0] == "status" {
		status, err := installer.Status()
		if err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		latest, err := p.Select(ctx, installer.Fetch, "https://api.github.com", "")
		if err != nil {
			status.LatestError = err.Error()
		} else {
			status.Latest = latest.Tag
		}
		data, _ := json.Marshal(status)
		fmt.Fprintln(r.out(), string(data))
		return 0
	}
	release, err := p.Select(ctx, installer.Fetch, "https://api.github.com", requested)
	if err != nil {
		fmt.Fprintln(r.errOut(), err)
		return 1
	}
	destination, err := installer.Install(ctx, release, takeOver)
	if err != nil {
		fmt.Fprintln(r.errOut(), err)
		return 1
	}
	data, _ := json.Marshal(map[string]string{"installed": destination, "tag": release.Tag})
	fmt.Fprintln(r.out(), string(data))
	return 0
}

func (r *runner) registerDistribution(ctx context.Context, store *plugin.Store, manifest string) (bool, error) {
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", r.socketPath())
	if err != nil {
		// Match plugin link's offline registration path, without resending an
		// ambiguous request after the server has accepted a connection.
		_, err := store.Link(manifest, true, nil)
		return true, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	err = api.WriteRequest(conn, api.Request{ID: "cli:distribution:register", Method: "plugin.link", Params: api.MarshalParams(map[string]any{"path": manifest, "enabled": true})})
	if err != nil {
		return true, err
	}
	line, err := readRawLine(bufio.NewReader(conn))
	if err != nil {
		return true, err
	}
	var response api.Response
	if err := json.Unmarshal(line, &response); err != nil {
		return true, err
	}
	if response.Err != nil {
		return true, response.Err
	}
	return true, nil
}

func verifyMacDialogSignature(root string) error {
	app := filepath.Join(root, "IkesuDecisionDialog.app")
	info, err := os.Stat(app)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("signed IkesuDecisionDialog.app is required")
	}
	requirement := `anchor apple generic and certificate leaf[subject.OU] = "67M8FDFZ28" and certificate leaf[subject.CN] = "Developer ID Application: KAZUHIRO HOMMA (67M8FDFZ28)"`
	cmd := exec.Command("codesign", "--verify", "--deep", "--strict", "-R", requirement, app)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Developer ID verification: %w: %s", err, output)
	}
	return nil
}
