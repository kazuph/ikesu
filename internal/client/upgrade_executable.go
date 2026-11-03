package client

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/childenv"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/guard"
)

const upgradeEnv = "IKESU_CLIENT_UPGRADE"

type upgradeSelection struct {
	Target  string                  `json:"target"`
	Session string                  `json:"session"`
	Value   *endpoint.ViewSelection `json:"value"`
}

type upgradeState struct {
	OldExecutable string                      `json:"old_executable"`
	OldVersion    string                      `json:"old_version"`
	Session       string                      `json:"session"`
	Attempt       upgradeIdentity             `json:"attempt"`
	Active        string                      `json:"active"`
	Selections    map[string]upgradeSelection `json:"selections"`
	Rollback      bool                        `json:"rollback,omitempty"`
	Reason        string                      `json:"reason,omitempty"`
}

func loadUpgradeState(session, version string) *upgradeState {
	raw := os.Getenv(upgradeEnv)
	_ = os.Unsetenv(upgradeEnv)
	if raw == "" {
		return nil
	}
	var state upgradeState
	if json.Unmarshal([]byte(raw), &state) != nil || state.Session != session || state.Attempt.Session != session {
		return nil
	}
	if (!state.Rollback && state.Attempt.Version != version) || (state.Rollback && state.OldVersion != version) {
		return nil
	}
	return &state
}

func execUpgrade(path string, state *upgradeState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	env := append(os.Environ(), upgradeEnv+"="+string(data))
	return syscall.Exec(path, os.Args, env)
}

func rollbackUpgrade(state *upgradeState, reason error) error {
	checked, err := checkUpgradeExecutable(context.Background(), state.OldExecutable, state.OldVersion)
	if err != nil {
		return fmt.Errorf("cannot restore previous client after %v: %w", reason, err)
	}
	if err := checked.recheck(); err != nil {
		return err
	}
	state.Rollback = true
	state.Reason = reason.Error()
	return execUpgrade(state.OldExecutable, state)
}

// upgradeIdentity identifies a server transition independently of its version.
type upgradeIdentity struct {
	Session    string `json:"session"`
	Generation string `json:"generation"`
	Executable string `json:"executable"`
	Version    string `json:"version"`
}

type upgradeCandidate struct {
	identity   upgradeIdentity
	executable *checkedExecutable
	state      upgradeState
}

type upgradeCheckResult struct {
	identity  upgradeIdentity
	candidate *upgradeCandidate
	err       error
}

func shouldCheckUpgrade(id upgradeIdentity, currentVersion string, failed map[upgradeIdentity]bool) bool {
	return id.Version != "" && id.Version != currentVersion && id.Executable != "" && id.Generation != "" && !failed[id]
}

func (m *machineClient) checkUpgrade(attempt uint64) {
	failed := make(map[upgradeIdentity]bool, len(m.failedUpgrades))
	for id := range m.failedUpgrades {
		failed[id] = true
	}
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		id, err := queryUpgradeIdentity(m.ctx, m.session)
		result := &upgradeCheckResult{identity: id, err: err}
		if err == nil && shouldCheckUpgrade(id, m.clientVersion, failed) {
			var checked *checkedExecutable
			checked, result.err = checkUpgradeExecutable(m.ctx, id.Executable, id.Version)
			if result.err == nil {
				oldPath, oldErr := os.Executable()
				if oldErr == nil {
					oldPath, oldErr = filepath.EvalSymlinks(oldPath)
				}
				if oldErr == nil {
					_, oldErr = checkUpgradeExecutable(m.ctx, oldPath, m.clientVersion)
				}
				if oldErr != nil {
					result.err = fmt.Errorf("current executable: %w", oldErr)
				} else {
					result.candidate = &upgradeCandidate{identity: id, executable: checked, state: upgradeState{OldExecutable: oldPath, OldVersion: m.clientVersion, Session: m.session, Attempt: id}}
				}
			}
		}
		m.post(m.ctx, endpointEvent{id: "local", attempt: attempt, upgrade: result})
	}()
}

func queryUpgradeIdentity(ctx context.Context, session string) (upgradeIdentity, error) {
	var id upgradeIdentity
	ctx, cancel := context.WithTimeout(ctx, attachTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", api.APISocketPath(api.SessionDir(session)))
	if err != nil {
		return id, err
	}
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return id, err
		}
	}
	if err := api.WriteRequest(conn, api.Request{ID: "client:upgrade", Method: api.MethodPing}); err != nil {
		return id, err
	}
	response, err := api.ReadResponse(bufio.NewReader(conn))
	if err != nil {
		return id, err
	}
	if response.Err != nil {
		return id, response.Err
	}
	var pong api.PongResult
	if err := json.Unmarshal(response.Result, &pong); err != nil {
		return id, err
	}
	if pong.Type != "pong" {
		return id, fmt.Errorf("invalid ping response")
	}
	return upgradeIdentity{Session: session, Generation: pong.Generation, Executable: pong.Executable, Version: pong.Version}, nil
}

type checkedExecutable struct {
	path string
	info os.FileInfo
	sum  [sha256.Size]byte
}

type versionOutput struct {
	data     []byte
	limit    int
	overflow bool
}

func (v *versionOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := v.limit - len(v.data)
	if len(p) > remaining {
		v.overflow = true
		p = p[:remaining]
	}
	v.data = append(v.data, p...)
	return n, nil
}

func executableIdentity(path string) (os.FileInfo, [sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	if err := guard.CheckExecutable(path); err != nil {
		return nil, sum, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, sum, err
	}
	for _, parent := range []string{filepath.Dir(path), filepath.Dir(resolved)} {
		for {
			info, err := os.Lstat(parent)
			if err != nil {
				return nil, sum, err
			}
			if info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0022 != 0 {
				stat, ok := info.Sys().(*syscall.Stat_t)
				if !ok || stat.Uid != 0 || info.Mode()&os.ModeSticky == 0 {
					return nil, sum, fmt.Errorf("writable executable parent: %s", parent)
				}
			}
			next := filepath.Dir(parent)
			if next == parent {
				break
			}
			parent = next
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, sum, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, sum, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return nil, sum, fmt.Errorf("executable is not owned by the current user")
	}
	pathInfo, err := os.Stat(path)
	if err != nil || !os.SameFile(info, pathInfo) {
		return nil, sum, fmt.Errorf("executable changed while opening")
	}
	if err := guard.CheckExecutable(path); err != nil {
		return nil, sum, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, sum, err
	}
	copy(sum[:], h.Sum(nil))
	return info, sum, nil
}

func checkUpgradeExecutable(ctx context.Context, path, version string) (*checkedExecutable, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("executable path is not absolute")
	}
	info, sum, err := executableIdentity(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("file missing: %w", err)
		}
		return nil, err
	}
	checked := &checkedExecutable{path: path, info: info, sum: sum}
	if err := checked.recheck(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, attachTimeout)
	defer cancel()
	cmd := childenv.CommandContext(ctx, path, "--version")
	cmd.WaitDelay = attachTimeout
	expected := "ikesu " + version
	out := &versionOutput{limit: len(expected) + len("\r\n")}
	cmd.Stdout = out
	err = cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("version timeout: %w", ctx.Err())
		}
		return nil, fmt.Errorf("version check failed: %w", err)
	}
	if out.overflow || strings.TrimSpace(string(out.data)) != expected {
		return nil, fmt.Errorf("version mismatch: server %s", version)
	}
	if err := checked.recheck(); err != nil {
		return nil, err
	}
	return checked, nil
}

func (c *checkedExecutable) recheck() error {
	info, sum, err := executableIdentity(c.path)
	if err != nil {
		return err
	}
	if !os.SameFile(c.info, info) || c.sum != sum {
		return fmt.Errorf("file changed after verification")
	}
	return nil
}
